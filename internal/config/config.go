package config

import "time"

// Configuration contains all the settings required by an Ingress controller
type Configuration struct {
	Namespace   string
	ResycPeriod time.Duration
}

// Config contains BFE config
type Config struct {
	//Namespace string
}

func NewConfig() *Config {
	return &Config{}
}
