package config

type Config struct {
	Endpoint         string
	KubeConfig       string
	NodeID           string
	HostStorageClass string
	Debug            bool
}
