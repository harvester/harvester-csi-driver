package csi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/container-storage-interface/spec/lib/go/csi"
	harvclient "github.com/harvester/harvester/pkg/generated/clientset/versioned"
	core "github.com/rancher/wrangler/v3/pkg/generated/controllers/core"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
)

// DeleteVolume tests.
//
// The real typed clients are pointed at an httptest server returning canned
// replies per "METHOD /path", so the client-go error-mapping path is exercised
// for real: a 403, a 404 with a metav1.Status body and a 404 with a plain-text
// body do not all become the same error. The fake records every request, which
// lets a test assert on the calls DeleteVolume did *not* make -- that is the
// actual regression for the shadow-PVC leak.

const (
	testNS  = "namespace-test-1"
	testVol = "pvc-2ed44b13-bba3-4dcd-8a2e-75e12a25dc38"
	testPV  = "pvc-f34ac8fb-8cff-4488-a4a5-e547a2887766"
)

// reply is a canned HTTP response for one "METHOD /path".
type reply struct {
	code int
	// body is JSON-encoded, with Content-Type: application/json.
	body interface{}
	// raw, when non-empty, is written verbatim as text/plain instead of body.
	// Used to mimic the apiserver's non-Status "404 page not found" for an API
	// group that is not served.
	raw string
}

func okReply(body interface{}) reply {
	return reply{code: http.StatusOK, body: body}
}

func statusReply(code int, reason metav1.StatusReason, msg string) reply {
	return reply{code: code, body: &metav1.Status{
		TypeMeta: metav1.TypeMeta{Kind: "Status", APIVersion: "v1"},
		Status:   metav1.StatusFailure,
		Code:     int32(code), //nolint:gosec // HTTP status codes always fit in int32
		Reason:   reason,
		Message:  msg,
	}}
}

func notFoundReply(msg string) reply {
	return statusReply(http.StatusNotFound, metav1.StatusReasonNotFound, msg)
}

func serverErrorReply(msg string) reply {
	return statusReply(http.StatusInternalServerError, metav1.StatusReasonInternalError, msg)
}

// fakeAPIServer serves canned replies and records every request it handles, so
// a test can assert which API calls DeleteVolume did or did not make.
type fakeAPIServer struct {
	mu     sync.Mutex
	routes map[string]reply
	seen   []string
}

func (f *fakeAPIServer) key(method, path string) string { return method + " " + path }

func (f *fakeAPIServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	k := f.key(r.Method, r.URL.Path)
	f.seen = append(f.seen, k)
	re, ok := f.routes[k]
	f.mu.Unlock()

	if !ok {
		// An unrouted path is a NotFound, same as a real apiserver.
		re = notFoundReply(fmt.Sprintf("no route for %s", k))
	}

	if re.raw != "" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(re.code)
		_, _ = w.Write([]byte(re.raw))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(re.code)
	_ = json.NewEncoder(w).Encode(re.body)
}

// setRoute replaces one route after the server has started, so a test can model
// state changing between two calls.
func (f *fakeAPIServer) setRoute(method, path string, re reply) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.routes[f.key(method, path)] = re
}

func (f *fakeAPIServer) resetCalls() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seen = nil
}

func (f *fakeAPIServer) count(method, path string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, s := range f.seen {
		if s == f.key(method, path) {
			n++
		}
	}
	return n
}

func (f *fakeAPIServer) called(method, path string) bool {
	return f.count(method, path) > 0
}

func (f *fakeAPIServer) totalCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.seen)
}

// indexOf returns the position of the first call to method/path, or -1.
func (f *fakeAPIServer) indexOf(method, path string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	want := f.key(method, path)
	for i, s := range f.seen {
		if s == want {
			return i
		}
	}
	return -1
}

func (f *fakeAPIServer) log() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.seen) == 0 {
		return "(no API calls)"
	}
	return strings.Join(f.seen, "\n  ")
}

func vrrPath(ns, name string) string {
	return "/apis/harvesterhci.io/v1beta1/namespaces/" + ns + "/volumeremoterestores/" + name
}

func pvcPath(ns, name string) string {
	return "/api/v1/namespaces/" + ns + "/persistentvolumeclaims/" + name
}

// shadowPVC is a Harvester-side "shadow" PVC as created by harvester-csi-driver
// for a guest-cluster volume. Deliberately RWO/Block so isLHRWXVolume()
// short-circuits and no StorageClass or NetworkFilesystem lookup is needed.
func shadowPVC(ns, name string) *corev1.PersistentVolumeClaim {
	blockMode := corev1.PersistentVolumeBlock
	return &corev1.PersistentVolumeClaim{
		TypeMeta: metav1.TypeMeta{Kind: "PersistentVolumeClaim", APIVersion: "v1"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			Finalizers: []string{
				"kubernetes.io/pvc-protection",
				"wrangler.cattle.io/persistentvolumeclaim-controller",
			},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			VolumeMode:  &blockMode,
			VolumeName:  testPV,
		},
		Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimBound},
	}
}

func volumeRemoteRestore(ns, name string) map[string]interface{} {
	return map[string]interface{}{
		"apiVersion": "harvesterhci.io/v1beta1",
		"kind":       "VolumeRemoteRestore",
		"metadata":   map[string]interface{}{"name": name, "namespace": ns},
		"spec":       map[string]interface{}{"type": "lh"},
	}
}

// baseRoutes is the ordinary case: no VolumeRemoteRestore exists for the
// volume, and the shadow PVC is present and deletable. Tests override
// individual entries to model the case under test.
func baseRoutes() map[string]reply {
	return map[string]reply{
		"GET " + vrrPath(testNS, testVol): notFoundReply(
			`volumeremoterestores.harvesterhci.io "` + testVol + `" not found`),
		"GET " + pvcPath(testNS, testVol):    okReply(shadowPVC(testNS, testVol)),
		"DELETE " + pvcPath(testNS, testVol): okReply(shadowPVC(testNS, testVol)),
	}
}

func newTestControllerServer(t *testing.T, routes map[string]reply) (*ControllerServer, *fakeAPIServer) {
	t.Helper()

	fake := &fakeAPIServer{routes: routes}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)

	cfg := &rest.Config{Host: srv.URL}

	coreFactory, err := core.NewFactoryFromConfig(cfg)
	if err != nil {
		t.Fatalf("core.NewFactoryFromConfig want err=<nil>, got err=%v", err)
	}
	harvClient, err := harvclient.NewForConfig(cfg)
	if err != nil {
		t.Fatalf("harvclient.NewForConfig want err=<nil>, got err=%v", err)
	}

	cs := &ControllerServer{
		namespace:  testNS,
		coreClient: coreFactory.Core().V1(),
		harvClient: harvClient,
		caps: getControllerServiceCapabilities([]csi.ControllerServiceCapability_RPC_Type{
			csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME,
		}),
	}
	return cs, fake
}

func deleteVolume(t *testing.T, cs *ControllerServer, volumeID string) error {
	t.Helper()
	_, err := cs.DeleteVolume(context.Background(), &csi.DeleteVolumeRequest{VolumeId: volumeID})
	return err
}

func wantOK(t *testing.T, err error, f *fakeAPIServer) {
	t.Helper()
	if err != nil {
		t.Fatalf("DeleteVolume want err=<nil>, got err=%v\ncalls:\n  %s", err, f.log())
	}
}

func wantCode(t *testing.T, err error, want codes.Code, f *fakeAPIServer) {
	t.Helper()
	if err == nil {
		t.Fatalf("DeleteVolume want code %v, got err=<nil>\ncalls:\n  %s", want, f.log())
	}
	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("DeleteVolume want a gRPC status error, got %T: %v\ncalls:\n  %s", err, err, f.log())
	}
	if st.Code() != want {
		t.Fatalf("DeleteVolume want code %v, got %v (%v)\ncalls:\n  %s", want, st.Code(), err, f.log())
	}
}

func wantCalled(t *testing.T, f *fakeAPIServer, method, path, why string) {
	t.Helper()
	if !f.called(method, path) {
		t.Fatalf("want a %s on %s (%s), but it was never issued\ncalls:\n  %s", method, path, why, f.log())
	}
}

func wantNotCalled(t *testing.T, f *fakeAPIServer, method, path, why string) {
	t.Helper()
	if f.called(method, path) {
		t.Fatalf("want no %s on %s (%s), but it was issued\ncalls:\n  %s", method, path, why, f.log())
	}
}

// --- argument validation ----------------------------------------------------

func TestDeleteVolume_EmptyVolumeID(t *testing.T) {
	cs, fake := newTestControllerServer(t, baseRoutes())

	err := deleteVolume(t, cs, "")

	wantCode(t, err, codes.InvalidArgument, fake)
	if n := fake.totalCalls(); n != 0 {
		t.Fatalf("want no API calls for an empty volume ID, got %d\ncalls:\n  %s", n, fake.log())
	}
}

func TestDeleteVolume_MissingCapability(t *testing.T) {
	cs, fake := newTestControllerServer(t, baseRoutes())
	// A controller without CREATE_DELETE_VOLUME must refuse the call.
	cs.caps = getControllerServiceCapabilities(nil)

	err := deleteVolume(t, cs, testVol)

	wantCode(t, err, codes.Internal, fake)
	wantNotCalled(t, fake, "DELETE", pvcPath(testNS, testVol), "the request was rejected up front")
}

// --- the ordinary path ------------------------------------------------------

// No VolumeRemoteRestore, shadow PVC present: DeleteVolume deletes the shadow
// PVC and succeeds. Control case -- if this fails, the harness itself is wrong
// and the other results mean nothing.
func TestDeleteVolume_DeletesShadowPVC(t *testing.T) {
	cs, fake := newTestControllerServer(t, baseRoutes())

	err := deleteVolume(t, cs, testVol)

	wantOK(t, err, fake)
	wantCalled(t, fake, "DELETE", pvcPath(testNS, testVol), "the shadow PVC must be reclaimed")
}

// --- issue #11905: DeleteVolume must be idempotent --------------------------

// CSI spec: "If the volume corresponding to the specified volume_id does not
// exist [...] the Plugin MUST reply 0 OK."
//
// Regression for the unreachable IsNotFound branch: the generic `err != nil`
// return used to come first, so a missing shadow PVC produced codes.Internal.
// external-provisioner retries a failed DeleteVolume forever, which left the
// guest PV Released permanently.
func TestDeleteVolume_ShadowPVCAlreadyGone(t *testing.T) {
	routes := baseRoutes()
	routes["GET "+pvcPath(testNS, testVol)] = notFoundReply(
		`persistentvolumeclaims "` + testVol + `" not found`)
	delete(routes, "DELETE "+pvcPath(testNS, testVol))

	cs, fake := newTestControllerServer(t, routes)

	err := deleteVolume(t, cs, testVol)

	wantOK(t, err, fake)
	wantNotCalled(t, fake, "DELETE", pvcPath(testNS, testVol), "there is nothing left to delete")
}

// Two DeleteVolume calls for the same volume, the second one after the shadow
// PVC is gone, must both report success. This is the sequence external-
// provisioner actually drives on a retry.
func TestDeleteVolume_IsIdempotentAcrossCalls(t *testing.T) {
	cs, fake := newTestControllerServer(t, baseRoutes())

	if err := deleteVolume(t, cs, testVol); err != nil {
		t.Fatalf("first DeleteVolume want err=<nil>, got err=%v\ncalls:\n  %s", err, fake.log())
	}
	wantCalled(t, fake, "DELETE", pvcPath(testNS, testVol), "the first call must delete the shadow PVC")

	// The shadow PVC is now gone; replay the same request.
	fake.setRoute("GET", pvcPath(testNS, testVol), notFoundReply(
		`persistentvolumeclaims "`+testVol+`" not found`))
	fake.resetCalls()

	err := deleteVolume(t, cs, testVol)

	wantOK(t, err, fake)
	wantNotCalled(t, fake, "DELETE", pvcPath(testNS, testVol), "the volume is already gone")
}

// The IsNotFound branch must not swallow other failures: a transient error
// reading the shadow PVC still has to surface so the call is retried. Without
// this, reordering the two checks could silently turn every lookup failure into
// a successful delete and strand the shadow PVC.
func TestDeleteVolume_ShadowPVCGetFails(t *testing.T) {
	routes := baseRoutes()
	routes["GET "+pvcPath(testNS, testVol)] = serverErrorReply("etcd leader changed")

	cs, fake := newTestControllerServer(t, routes)

	err := deleteVolume(t, cs, testVol)

	wantCode(t, err, codes.Internal, fake)
	wantNotCalled(t, fake, "DELETE", pvcPath(testNS, testVol), "the PVC could not be read")
}

// The shadow PVC disappearing between the Get and the Delete is a benign race,
// not a failure.
func TestDeleteVolume_ShadowPVCDeletedConcurrently(t *testing.T) {
	routes := baseRoutes()
	routes["DELETE "+pvcPath(testNS, testVol)] = notFoundReply(
		`persistentvolumeclaims "` + testVol + `" not found`)

	cs, fake := newTestControllerServer(t, routes)

	err := deleteVolume(t, cs, testVol)

	wantOK(t, err, fake)
}

func TestDeleteVolume_ShadowPVCDeleteFails(t *testing.T) {
	routes := baseRoutes()
	routes["DELETE "+pvcPath(testNS, testVol)] = serverErrorReply("etcd leader changed")

	cs, fake := newTestControllerServer(t, routes)

	err := deleteVolume(t, cs, testVol)

	wantCode(t, err, codes.Internal, fake)
}

// --- VolumeRemoteRestore handling -------------------------------------------

// Regression for the shadow-PVC leak: DeleteVolume used to return OK as soon as
// it had deleted the VolumeRemoteRestore, so the Harvester-side PVC was never
// touched. external-provisioner then removed the guest PV, leaving a shadow PVC
// with no guest counterpart and nothing left to reclaim it.
func TestDeleteVolume_VRRExists_DeletesVRRAndShadowPVC(t *testing.T) {
	routes := baseRoutes()
	routes["GET "+vrrPath(testNS, testVol)] = okReply(volumeRemoteRestore(testNS, testVol))
	routes["DELETE "+vrrPath(testNS, testVol)] = okReply(volumeRemoteRestore(testNS, testVol))

	cs, fake := newTestControllerServer(t, routes)

	err := deleteVolume(t, cs, testVol)

	wantOK(t, err, fake)
	wantCalled(t, fake, "DELETE", vrrPath(testNS, testVol), "the VolumeRemoteRestore must be removed")
	wantCalled(t, fake, "DELETE", pvcPath(testNS, testVol),
		"deleting the VolumeRemoteRestore does not reclaim the shadow PVC")

	// The VolumeRemoteRestore has to go first: it owns the PVC, so deleting the
	// PVC while it still exists would let the restore controller recreate it.
	if vrrAt, pvcAt := fake.indexOf("DELETE", vrrPath(testNS, testVol)),
		fake.indexOf("DELETE", pvcPath(testNS, testVol)); vrrAt > pvcAt {
		t.Fatalf("want the VolumeRemoteRestore deleted before the shadow PVC\ncalls:\n  %s", fake.log())
	}
}

// Deleting the VolumeRemoteRestore failing must abort with a gRPC status, not a
// raw client error, and must not delete the shadow PVC -- the restore object
// still references it.
func TestDeleteVolume_VRRDeleteFails(t *testing.T) {
	routes := baseRoutes()
	routes["GET "+vrrPath(testNS, testVol)] = okReply(volumeRemoteRestore(testNS, testVol))
	routes["DELETE "+vrrPath(testNS, testVol)] = serverErrorReply("etcd leader changed")

	cs, fake := newTestControllerServer(t, routes)

	err := deleteVolume(t, cs, testVol)

	wantCode(t, err, codes.Internal, fake)
	wantNotCalled(t, fake, "DELETE", pvcPath(testNS, testVol), "the VolumeRemoteRestore is still present")
}

// A transient failure reading the VolumeRemoteRestore is retryable, so
// DeleteVolume must surface it rather than deleting the shadow PVC while a
// VolumeRemoteRestore may still exist for it.
//
// NOTE: this also documents the current behaviour for a 403 on this resource,
// which aborts the delete in the same way. Whether an unauthorized or unserved
// VolumeRemoteRestore API should instead be skipped is tracked separately; if
// that changes, split this test and assert success for the 403 case only.
func TestDeleteVolume_VRRGetFails(t *testing.T) {
	routes := baseRoutes()
	routes["GET "+vrrPath(testNS, testVol)] = serverErrorReply("etcd leader changed")

	cs, fake := newTestControllerServer(t, routes)

	err := deleteVolume(t, cs, testVol)

	wantCode(t, err, codes.Internal, fake)
	wantNotCalled(t, fake, "GET", pvcPath(testNS, testVol), "the call aborted before reading the PVC")
}

// A cluster that does not serve the harvesterhci.io VolumeRemoteRestore
// resource at all answers with a plain-text "404 page not found" rather than a
// metav1.Status. client-go maps that to NotFound, so DeleteVolume takes the
// "no VolumeRemoteRestore exists" path and proceeds -- a missing CRD must not
// block volume deletion.
func TestDeleteVolume_VRRAPINotServed(t *testing.T) {
	routes := baseRoutes()
	routes["GET "+vrrPath(testNS, testVol)] = reply{
		code: http.StatusNotFound,
		raw:  "404 page not found\n",
	}

	cs, fake := newTestControllerServer(t, routes)

	err := deleteVolume(t, cs, testVol)

	wantOK(t, err, fake)
	wantCalled(t, fake, "DELETE", pvcPath(testNS, testVol),
		"an unserved VolumeRemoteRestore API must not block deletion")
}
