package csi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/container-storage-interface/spec/lib/go/csi"
	harvesterv1beta1 "github.com/harvester/harvester/pkg/apis/harvesterhci.io/v1beta1"
	harvclient "github.com/harvester/harvester/pkg/generated/clientset/versioned"
	harvtypedv1beta1 "github.com/harvester/harvester/pkg/generated/clientset/versioned/typed/harvesterhci.io/v1beta1"
	core "github.com/rancher/wrangler/v3/pkg/generated/controllers/core"
	ctlv1 "github.com/rancher/wrangler/v3/pkg/generated/controllers/core/v1"
	"github.com/sirupsen/logrus"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
)

// --- VolumeRemoteRestore lookup-error handling (harvester/harvester#11905, BUG 2) ---
//
// These tests drive the error branches of deleteVolumeRemoteRestoreIfExists through
// injected fake clients rather than the httptest server above. A fake returns a
// constructed apierror directly, which is what lets a NoMatch error -- something no
// HTTP status can express -- be covered alongside the 403 and the transient failure.

const (
	testNamespace = "test-namespace"
	testVolumeID  = "pvc-12345678-1234-1234-1234-123456789012"
)

var (
	vrrGroupResource = schema.GroupResource{Group: "harvesterhci.io", Resource: "volumeremoterestores"}
	pvcGroupResource = schema.GroupResource{Group: "", Resource: "persistentvolumeclaims"}
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
type fakeVRRClient struct {
	harvtypedv1beta1.VolumeRemoteRestoreInterface

	vrr       *harvesterv1beta1.VolumeRemoteRestore
	getErr    error
	deleteErr error

	getCalls    int
	deleteCalls int
}

type fakeHarvV1beta1Client struct {
	harvtypedv1beta1.HarvesterhciV1beta1Interface

	vrrClient *fakeVRRClient
}

type fakeHarvClient struct {
	harvclient.Interface

	v1beta1 *fakeHarvV1beta1Client
}
type fakePVCController struct {
	ctlv1.PersistentVolumeClaimController

	pvc       *corev1.PersistentVolumeClaim
	getErr    error
	deleteErr error

	getCalls    int
	deleteCalls int
}
type fakeCoreClient struct {
	ctlv1.Interface

	pvcController *fakePVCController
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

// wantErr asserts only that the call failed. The VolumeRemoteRestore paths still
// return the raw client error rather than a gRPC status; converting those is the
// subject of a separate change, so these tests pin the abort, not its type.
func wantErr(t *testing.T, err error, f *fakeAPIServer) {
	t.Helper()
	if err == nil {
		t.Fatalf("DeleteVolume want an error, got err=<nil>\ncalls:\n  %s", f.log())
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

// A volume restored from a backup is represented by a VolumeRemoteRestore that
// owns its PVC through a controller owner reference, so deleting the restore
// cascades to the PVC. DeleteVolume deletes the restore and returns, leaving the
// PVC to garbage collection rather than racing it with a delete of its own.
func TestDeleteVolume_VRRExists_DeletesVRR(t *testing.T) {
	routes := baseRoutes()
	routes["GET "+vrrPath(testNS, testVol)] = okReply(volumeRemoteRestore(testNS, testVol))
	routes["DELETE "+vrrPath(testNS, testVol)] = okReply(volumeRemoteRestore(testNS, testVol))

	cs, fake := newTestControllerServer(t, routes)

	err := deleteVolume(t, cs, testVol)

	wantOK(t, err, fake)
	wantCalled(t, fake, "DELETE", vrrPath(testNS, testVol), "the VolumeRemoteRestore must be removed")
	wantNotCalled(t, fake, "DELETE", pvcPath(testNS, testVol),
		"the cascading delete of the VolumeRemoteRestore reclaims the PVC")
}

// Deleting the VolumeRemoteRestore failing must abort, and must not delete the
// shadow PVC -- the restore object still references it.
func TestDeleteVolume_VRRDeleteFails(t *testing.T) {
	routes := baseRoutes()
	routes["GET "+vrrPath(testNS, testVol)] = okReply(volumeRemoteRestore(testNS, testVol))
	routes["DELETE "+vrrPath(testNS, testVol)] = serverErrorReply("etcd leader changed")

	cs, fake := newTestControllerServer(t, routes)

	err := deleteVolume(t, cs, testVol)

	wantErr(t, err, fake)
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

	wantErr(t, err, fake)
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

// errForbiddenVRR mimics what the API server returns when the harvesterhci.io:csi-driver
// ClusterRole is missing the volumeremoterestores permissions, which is the failure that
// made DeleteVolume abort for every volume (harvester/harvester#11905, BUG 2).
func errForbiddenVRR(name string) error {
	return errors.NewForbidden(vrrGroupResource, name,
		fmt.Errorf("volumeremoterestores.harvesterhci.io is forbidden: "+
			"User \"system:serviceaccount:default:harvester-csi-driver\" cannot get resource "+
			"\"volumeremoterestores\" in API group \"harvesterhci.io\" in the namespace %q", testNamespace))
}

// errNoKindMatchVRR mimics a host cluster that predates the VolumeRemoteRestore CRD.
func errNoKindMatchVRR() error {
	return &meta.NoKindMatchError{
		GroupKind:        schema.GroupKind{Group: "harvesterhci.io", Kind: "VolumeRemoteRestore"},
		SearchedVersions: []string{"v1beta1"},
	}
}

func (f *fakeVRRClient) Get(_ context.Context, name string, _ metav1.GetOptions) (*harvesterv1beta1.VolumeRemoteRestore, error) {
	f.getCalls++
	if f.getErr != nil {
		return nil, f.getErr
	}
	if f.vrr == nil {
		return nil, errors.NewNotFound(vrrGroupResource, name)
	}
	return f.vrr, nil
}

func (f *fakeVRRClient) Delete(_ context.Context, _ string, _ metav1.DeleteOptions) error {
	f.deleteCalls++
	return f.deleteErr
}

func (f *fakeHarvV1beta1Client) VolumeRemoteRestores(string) harvtypedv1beta1.VolumeRemoteRestoreInterface {
	return f.vrrClient
}

func (f *fakeHarvClient) HarvesterhciV1beta1() harvtypedv1beta1.HarvesterhciV1beta1Interface {
	return f.v1beta1
}

type fakeDiscovery struct {
	resources *metav1.APIResourceList
	err       error

	calls int
}

func (f *fakeDiscovery) ServerResourcesForGroupVersion(_ string) (*metav1.APIResourceList, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return f.resources, nil
}

func harvesterAPIResources(names ...string) *metav1.APIResourceList {
	list := &metav1.APIResourceList{GroupVersion: harvesterv1beta1.SchemeGroupVersion.String()}
	for _, name := range names {
		list.APIResources = append(list.APIResources, metav1.APIResource{Name: name})
	}
	return list
}

// discoveryWithVRR mimics Harvester v1.8 and later, which serves the VolumeRemoteRestore API.
func discoveryWithVRR() *fakeDiscovery {
	return &fakeDiscovery{resources: harvesterAPIResources("settings", "volumeremotebackups", volumeRemoteRestoreResource)}
}

// discoveryWithoutVRR mimics Harvester v1.7 and earlier: harvesterhci.io/v1beta1 is served,
// but it has no volumeremoterestores resource in it.
func discoveryWithoutVRR() *fakeDiscovery {
	return &fakeDiscovery{resources: harvesterAPIResources("settings", "virtualmachineimages")}
}

// discoveryWithoutGroup mimics a host that does not serve harvesterhci.io/v1beta1 at all.
func discoveryWithoutGroup() *fakeDiscovery {
	return &fakeDiscovery{err: errors.NewNotFound(
		schema.GroupResource{Group: "harvesterhci.io", Resource: "v1beta1"}, "")}
}

// discoveryUnavailable mimics discovery itself failing, which leaves the driver unable to tell
// a pre-v1.8 host apart from a broken RBAC rule.
func discoveryUnavailable() *fakeDiscovery {
	return &fakeDiscovery{err: errors.NewServiceUnavailable("discovery is down")}
}

func (f *fakePVCController) Get(_, name string, _ metav1.GetOptions) (*corev1.PersistentVolumeClaim, error) {
	f.getCalls++
	if f.getErr != nil {
		return nil, f.getErr
	}
	if f.pvc == nil {
		return nil, errors.NewNotFound(pvcGroupResource, name)
	}
	return f.pvc, nil
}

func (f *fakePVCController) Delete(_, _ string, _ *metav1.DeleteOptions) error {
	f.deleteCalls++
	return f.deleteErr
}

func (f *fakeCoreClient) PersistentVolumeClaim() ctlv1.PersistentVolumeClaimController {
	return f.pvcController
}

// blockPVC returns a PVC that isLHRWXVolume() rejects without consulting the StorageClass
// client, so DeleteVolume tests can stay focused on the VolumeRemoteRestore lookup.
func blockPVC(name string) *corev1.PersistentVolumeClaim {
	return &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: name},
		Spec: corev1.PersistentVolumeClaimSpec{
			VolumeMode:  ptr.To(corev1.PersistentVolumeBlock),
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
		},
	}
}

func newFakeClientControllerServer(
	vrrClient *fakeVRRClient,
	pvcController *fakePVCController,
	discovery *fakeDiscovery,
) *ControllerServer {
	return &ControllerServer{
		namespace:     testNamespace,
		coreClient:    &fakeCoreClient{pvcController: pvcController},
		harvClient:    &fakeHarvClient{v1beta1: &fakeHarvV1beta1Client{vrrClient: vrrClient}},
		hostDiscovery: discovery,
		caps: getControllerServiceCapabilities(
			[]csi.ControllerServiceCapability_RPC_Type{
				csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME,
			},
		),
	}
}

// TestDeleteVolumeRemoteRestoreIfExists covers harvester/harvester#11905 BUG 2: a
// VolumeRemoteRestore lookup that cannot succeed because the host does not serve the API must
// not abort the volume deletion, while an error that may clear up on its own, and a denial on
// a host that does serve the API, must still be surfaced so the external-provisioner retries.
func TestDeleteVolumeRemoteRestoreIfExists(t *testing.T) {
	testCases := []struct {
		name            string
		vrrClient       *fakeVRRClient
		discovery       *fakeDiscovery
		wantDeleted     bool
		wantCode        codes.Code
		wantDeleteCalls int
	}{
		{
			name:      "no VolumeRemoteRestore exists",
			vrrClient: &fakeVRRClient{},
			discovery: discoveryWithVRR(),
		},
		{
			// Harvester v1.7 and earlier evaluate RBAC before routing, so a host without the
			// CRD answers 403 rather than 404. Discovery is what tells the two apart.
			name:      "lookup forbidden on a host without the API",
			vrrClient: &fakeVRRClient{getErr: errForbiddenVRR(testVolumeID)},
			discovery: discoveryWithoutVRR(),
		},
		{
			name:      "lookup forbidden on a host without the API group",
			vrrClient: &fakeVRRClient{getErr: errForbiddenVRR(testVolumeID)},
			discovery: discoveryWithoutGroup(),
		},
		{
			// A supported host serves the API, so a denial is a stale or incomplete RBAC
			// rule. Skipping it would orphan a VolumeRemoteRestore that really exists.
			name:      "lookup forbidden on a host that serves the API",
			vrrClient: &fakeVRRClient{getErr: errForbiddenVRR(testVolumeID)},
			discovery: discoveryWithVRR(),
			wantCode:  codes.Internal,
		},
		{
			// Discovery could not confirm the API is missing, so the denial is not safe to
			// skip either.
			name:      "lookup forbidden and discovery is unavailable",
			vrrClient: &fakeVRRClient{getErr: errForbiddenVRR(testVolumeID)},
			discovery: discoveryUnavailable(),
			wantCode:  codes.Internal,
		},
		{
			name:      "VolumeRemoteRestore CRD is not installed",
			vrrClient: &fakeVRRClient{getErr: errNoKindMatchVRR()},
			discovery: discoveryWithVRR(),
		},
		{
			name:      "lookup fails with a transient error",
			vrrClient: &fakeVRRClient{getErr: errors.NewServerTimeout(vrrGroupResource, "get", 1)},
			discovery: discoveryWithVRR(),
			wantCode:  codes.Internal,
		},
		{
			name: "VolumeRemoteRestore exists and is deleted",
			vrrClient: &fakeVRRClient{
				vrr: &harvesterv1beta1.VolumeRemoteRestore{
					ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: testVolumeID},
				},
			},
			discovery:       discoveryWithVRR(),
			wantDeleted:     true,
			wantDeleteCalls: 1,
		},
		{
			// Deleted concurrently between the get and the delete: not an error, and the
			// caller must still go on to delete the PVC, so deleted stays false.
			name: "VolumeRemoteRestore disappears before the delete",
			vrrClient: &fakeVRRClient{
				vrr: &harvesterv1beta1.VolumeRemoteRestore{
					ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: testVolumeID},
				},
				deleteErr: errors.NewNotFound(vrrGroupResource, testVolumeID),
			},
			discovery:       discoveryWithVRR(),
			wantDeleteCalls: 1,
		},
		{
			name: "VolumeRemoteRestore exists but deletion fails",
			vrrClient: &fakeVRRClient{
				vrr: &harvesterv1beta1.VolumeRemoteRestore{
					ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: testVolumeID},
				},
				deleteErr: errForbiddenVRR(testVolumeID),
			},
			discovery:       discoveryWithVRR(),
			wantCode:        codes.Internal,
			wantDeleteCalls: 1,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cs := newFakeClientControllerServer(tc.vrrClient, &fakePVCController{}, tc.discovery)

			deleted, err := cs.deleteVolumeRemoteRestoreIfExists(context.TODO(), testVolumeID)

			// status.Code() reports Unknown for a plain client error, so this also asserts
			// that the error reaching the CSI caller is a gRPC status.
			if gotCode := status.Code(err); gotCode != tc.wantCode {
				t.Errorf("deleteVolumeRemoteRestoreIfExists() want code=%v, got code=%v (err=%v)", tc.wantCode, gotCode, err)
			}
			if deleted != tc.wantDeleted {
				t.Errorf("deleteVolumeRemoteRestoreIfExists() want deleted=%v, got deleted=%v", tc.wantDeleted, deleted)
			}
			if tc.vrrClient.getCalls != 1 {
				t.Errorf("VolumeRemoteRestore Get want calls=1, got calls=%d", tc.vrrClient.getCalls)
			}
			if tc.vrrClient.deleteCalls != tc.wantDeleteCalls {
				t.Errorf("VolumeRemoteRestore Delete want calls=%d, got calls=%d", tc.wantDeleteCalls, tc.vrrClient.deleteCalls)
			}
			// Discovery is only consulted to disambiguate a denial, so it must stay off the
			// path every other delete takes.
			wantDiscoveryCalls := 0
			if errors.IsForbidden(tc.vrrClient.getErr) {
				wantDiscoveryCalls = 1
			}
			if tc.discovery.calls != wantDiscoveryCalls {
				t.Errorf("discovery want calls=%d, got calls=%d", wantDiscoveryCalls, tc.discovery.calls)
			}
		})
	}
}

// TestDeleteVolumeWithUnavailableVolumeRemoteRestoreAPI is the end-to-end regression test for
// harvester/harvester#11905 BUG 2: before the fix a 403 (or a missing CRD) on the
// VolumeRemoteRestore lookup aborted DeleteVolume, so the host PVC was never deleted and the
// guest PV stayed Released forever.
func TestDeleteVolumeWithUnavailableVolumeRemoteRestoreAPI(t *testing.T) {
	testCases := []struct {
		name      string
		getErr    error
		discovery *fakeDiscovery
	}{
		{
			name:      "lookup forbidden on a host without the API",
			getErr:    errForbiddenVRR(testVolumeID),
			discovery: discoveryWithoutVRR(),
		},
		{
			name:      "lookup forbidden on a host without the API group",
			getErr:    errForbiddenVRR(testVolumeID),
			discovery: discoveryWithoutGroup(),
		},
		{
			name:      "VolumeRemoteRestore CRD is not installed",
			getErr:    errNoKindMatchVRR(),
			discovery: discoveryWithVRR(),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			vrrClient := &fakeVRRClient{getErr: tc.getErr}
			pvcController := &fakePVCController{pvc: blockPVC(testVolumeID)}
			cs := newFakeClientControllerServer(vrrClient, pvcController, tc.discovery)

			resp, err := cs.DeleteVolume(context.TODO(), &csi.DeleteVolumeRequest{VolumeId: testVolumeID})

			if err != nil {
				t.Fatalf("DeleteVolume() want err=<nil>, got err=%v", err)
			}
			if resp == nil {
				t.Fatal("DeleteVolume() want resp=non-nil, got resp=<nil>")
			}
			if pvcController.deleteCalls != 1 {
				t.Errorf("PVC Delete want calls=1, got calls=%d", pvcController.deleteCalls)
			}
			if vrrClient.deleteCalls != 0 {
				t.Errorf("VolumeRemoteRestore Delete want calls=0, got calls=%d", vrrClient.deleteCalls)
			}
		})
	}
}

// TestDeleteVolumeWithTransientVolumeRemoteRestoreError makes sure the BUG 2 fix did not turn
// every lookup failure into a silent skip: an error that may clear up on its own must abort
// DeleteVolume so the external-provisioner retries instead of leaking the VolumeRemoteRestore.
func TestDeleteVolumeWithTransientVolumeRemoteRestoreError(t *testing.T) {
	vrrClient := &fakeVRRClient{getErr: errors.NewServerTimeout(vrrGroupResource, "get", 1)}
	pvcController := &fakePVCController{pvc: blockPVC(testVolumeID)}
	cs := newFakeClientControllerServer(vrrClient, pvcController, discoveryWithVRR())

	_, err := cs.DeleteVolume(context.TODO(), &csi.DeleteVolumeRequest{VolumeId: testVolumeID})
	if gotCode := status.Code(err); gotCode != codes.Internal {
		t.Errorf("DeleteVolume() want code=%v, got code=%v (err=%v)", codes.Internal, gotCode, err)
	}
	if pvcController.getCalls != 0 {
		t.Errorf("PVC Get want calls=0, got calls=%d", pvcController.getCalls)
	}
	if pvcController.deleteCalls != 0 {
		t.Errorf("PVC Delete want calls=0, got calls=%d", pvcController.deleteCalls)
	}
}

// TestDeleteVolumeDeletesVolumeRemoteRestore guards the behaviour the BUG 2 fix had to keep:
// when the VolumeRemoteRestore is found it is deleted and the host PVC is left alone, because
// removing the VolumeRemoteRestore cascades to the PVC it owns.
func TestDeleteVolumeDeletesVolumeRemoteRestore(t *testing.T) {
	vrrClient := &fakeVRRClient{
		vrr: &harvesterv1beta1.VolumeRemoteRestore{
			ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: testVolumeID},
		},
	}
	pvcController := &fakePVCController{pvc: blockPVC(testVolumeID)}
	cs := newFakeClientControllerServer(vrrClient, pvcController, discoveryWithVRR())

	if _, err := cs.DeleteVolume(context.TODO(), &csi.DeleteVolumeRequest{VolumeId: testVolumeID}); err != nil {
		t.Fatalf("DeleteVolume() want err=<nil>, got err=%v", err)
	}
	if vrrClient.deleteCalls != 1 {
		t.Errorf("VolumeRemoteRestore Delete want calls=1, got calls=%d", vrrClient.deleteCalls)
	}
	if pvcController.getCalls != 0 {
		t.Errorf("PVC Get want calls=0, got calls=%d", pvcController.getCalls)
	}
}

// TestDeleteVolumeWithConcurrentlyDeletedVolumeRemoteRestore covers the same idempotency hole
// as BUG 2, on the delete call instead of the lookup: when the VolumeRemoteRestore is removed
// between the get and the delete, DeleteVolume must fall through to the host PVC rather than
// fail, otherwise a PVC the cascading delete left behind is never collected.
func TestDeleteVolumeWithConcurrentlyDeletedVolumeRemoteRestore(t *testing.T) {
	vrrClient := &fakeVRRClient{
		vrr: &harvesterv1beta1.VolumeRemoteRestore{
			ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: testVolumeID},
		},
		deleteErr: errors.NewNotFound(vrrGroupResource, testVolumeID),
	}
	pvcController := &fakePVCController{pvc: blockPVC(testVolumeID)}
	cs := newFakeClientControllerServer(vrrClient, pvcController, discoveryWithVRR())

	if _, err := cs.DeleteVolume(context.TODO(), &csi.DeleteVolumeRequest{VolumeId: testVolumeID}); err != nil {
		t.Fatalf("DeleteVolume() want err=<nil>, got err=%v", err)
	}
	if pvcController.deleteCalls != 1 {
		t.Errorf("PVC Delete want calls=1, got calls=%d", pvcController.deleteCalls)
	}
}

// TestDeleteVolumeWithForbiddenLookupOnSupportedHost is the counterpart to
// TestDeleteVolumeWithUnavailableVolumeRemoteRestoreAPI: on a host that does serve the API a
// denial means the RBAC rule is wrong, not that the API is missing. Deleting the host PVC
// anyway would leave the VolumeRemoteRestore behind to recreate it, so DeleteVolume has to
// fail and let the external-provisioner retry.
func TestDeleteVolumeWithForbiddenLookupOnSupportedHost(t *testing.T) {
	vrrClient := &fakeVRRClient{getErr: errForbiddenVRR(testVolumeID)}
	pvcController := &fakePVCController{pvc: blockPVC(testVolumeID)}
	cs := newFakeClientControllerServer(vrrClient, pvcController, discoveryWithVRR())

	_, err := cs.DeleteVolume(context.TODO(), &csi.DeleteVolumeRequest{VolumeId: testVolumeID})
	if gotCode := status.Code(err); gotCode != codes.Internal {
		t.Errorf("DeleteVolume() want code=%v, got code=%v (err=%v)", codes.Internal, gotCode, err)
	}
	if pvcController.deleteCalls != 0 {
		t.Errorf("PVC Delete want calls=0, got calls=%d", pvcController.deleteCalls)
	}
}

// TestDeleteVolumeRemoteRestoreIfExistsLogging keeps the logs honest: a volume that was simply
// never restored from a backup is the common case and must not be reported as a problem, while
// an unusable VolumeRemoteRestore API must be warned about, since that is the signal an
// operator needs to spot the missing RBAC rule.
func TestDeleteVolumeRemoteRestoreIfExistsLogging(t *testing.T) {
	const unavailableMsg = "VolumeRemoteRestore API unavailable"

	testCases := []struct {
		name      string
		getErr    error
		discovery *fakeDiscovery
		wantWarn  bool
	}{
		{
			name:      "no VolumeRemoteRestore exists",
			discovery: discoveryWithVRR(),
		},
		{
			name:      "lookup forbidden on a host without the API",
			getErr:    errForbiddenVRR(testVolumeID),
			discovery: discoveryWithoutVRR(),
			wantWarn:  true,
		},
		{
			name:      "VolumeRemoteRestore CRD is not installed",
			getErr:    errNoKindMatchVRR(),
			discovery: discoveryWithVRR(),
			wantWarn:  true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var logs bytes.Buffer
			origOut := logrus.StandardLogger().Out
			logrus.SetOutput(&logs)
			defer logrus.SetOutput(origOut)

			cs := newFakeClientControllerServer(&fakeVRRClient{getErr: tc.getErr}, &fakePVCController{}, tc.discovery)

			if _, err := cs.deleteVolumeRemoteRestoreIfExists(context.TODO(), testVolumeID); err != nil {
				t.Fatalf("deleteVolumeRemoteRestoreIfExists() want err=<nil>, got err=%v", err)
			}

			if gotWarn := strings.Contains(logs.String(), unavailableMsg); gotWarn != tc.wantWarn {
				t.Errorf("logs containing %q want %v, got %v (logs=%q)", unavailableMsg, tc.wantWarn, gotWarn, logs.String())
			}
		})
	}
}
