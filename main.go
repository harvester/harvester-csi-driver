package main

import (
	_ "net/http/pprof"
	"os"

	"github.com/harvester/harvester-csi-driver/pkg/config"
	"github.com/harvester/harvester-csi-driver/pkg/csi"
	"github.com/harvester/harvester-csi-driver/pkg/version"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli"
)

var (
	cfg config.Config
)

func main() {
	flags := []cli.Flag{
		cli.StringFlag{
			Name:        "endpoint",
			Value:       "unix:///csi/csi.sock",
			Usage:       "CSI endpoint",
			Destination: &cfg.Endpoint,
		},
		cli.StringFlag{
			Name:        "nodeid",
			Usage:       "Node ID",
			Destination: &cfg.NodeID,
		},
		cli.StringFlag{
			Name:        "kubeconfig",
			Value:       "",
			Usage:       "kubeconfig to access host Harvester cluster",
			Destination: &cfg.KubeConfig,
		},
	}

	app := cli.NewApp()

	app.Name = "harvester CSI driver"
	app.Version = version.FriendlyVersion()
	app.Flags = flags
	app.Action = func(c *cli.Context) {
		if err := runCSI(c); err != nil {
			logrus.Fatalf("Error running CSI driver: %v", err)
		}
	}

	if err := app.Run(os.Args); err != nil {
		logrus.Fatalf("Error running CSI driver: %v", err)
	}
}

func runCSI(c *cli.Context) error {
	manager := csi.GetCSIManager()
	return manager.Run(&cfg)
}
