package config

// Configuration contains all the settings required by an Ingress controller
type Configuration struct {
	Namespace string
}

// Config contains BFE config
type Config struct {
	//Namespace string
}

func NewConfig() *Config {
	return &Config{

	}
}