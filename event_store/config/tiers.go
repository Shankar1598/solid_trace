package config

type StorageTier struct {
	Kind     string `yaml:"kind"`
	Locator  string `yaml:"locator"`
	Bucket   string `yaml:"bucket"`
	Prefix   string `yaml:"prefix"` // optional
	Level    int    `yaml:"level"`
	Endpoint string `yaml:"endpoint"` // optional for emulators
}
