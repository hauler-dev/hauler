package registry

type RegistryConfig struct {}

type Config struct {
	Layout Layout `json:"layout,omitempty"`
	Proxy Proxy `json:"proxy,omitempty"`
}

type Layout struct {
	Root string
}

type Proxy struct {
	Remotes []Remotes `json:"remotes,omitempty"`
}

type Remotes struct {}
