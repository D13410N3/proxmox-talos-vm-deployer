package main

// YAML config types.
type BaseTemplate struct {
	Name string `yaml:"name"`
	ID   int    `yaml:"id"`
}

type CoreRange struct {
	Phy string `yaml:"phy"`
	HT  string `yaml:"ht"`
}

type NumaNode struct {
	ID    int       `yaml:"id"`
	Cores CoreRange `yaml:"cores"`
}

type NodeConfig struct {
	Name          string         `yaml:"name"`
	Weight        int            `yaml:"weight"`
	Suffix        string         `yaml:"suffix"`
	HT            bool           `yaml:"ht"`
	Hugepages     bool           `yaml:"hugepages"`
	NUMA          []NumaNode     `yaml:"numa"`
	BaseTemplates []BaseTemplate `yaml:"base_templates"`
}

type VmTemplate struct {
	Name     string `yaml:"name"`
	CPU      int    `yaml:"cpu"`
	Memory   int    `yaml:"memory"`
	Disk     int    `yaml:"disk"`
	CPUModel string `yaml:"cpu_model"`
	Role     string `yaml:"role"` // worker or controlplane
	NUMA     string `yaml:"numa,omitempty"`
	PhyCores string `yaml:"phy,omitempty"`
	HTCores  string `yaml:"ht,omitempty"`
}

type Config struct {
	Nodes       []NodeConfig `yaml:"nodes"`
	VmTemplates []VmTemplate `yaml:"vm_templates"`
}

type AppConfig struct {
	ProxmoxBaseAddr           string `env:"PROXMOX_BASE_ADDR,required"`
	ProxmoxToken              string `env:"PROXMOX_TOKEN,required"`
	SentryDSN                 string `env:"SENTRY_DSN,required"`
	ListenAddr                string `env:"LISTEN_ADDR,required"`
	ListenPort                string `env:"LISTEN_PORT,required"`
	ConfigPath                string `env:"CONFIG_PATH,required"`
	AuthToken                 string `env:"AUTH_TOKEN,required"`
	TalosMachineTemplate      string `env:"TALOS_MACHINE_TEMPLATE,required"`
	TalosControlPlaneEndpoint string `env:"TALOS_CONTROLPLANE_ENDPOINT,required"`
	MikrotikIP                string `env:"MIKROTIK_IP,required"`
	MikrotikPort              string `env:"MIKROTIK_PORT" envDefault:"8080"`
	MikrotikUsername          string `env:"MIKROTIK_USERNAME,required"`
	MikrotikPassword          string `env:"MIKROTIK_PASSWORD,required"`
	Debug                     bool   `env:"DEBUG" envDefault:"false"`
	LogLevel                  int    `env:"LOG_LEVEL" envDefault:"1"`     // 0: Debug, 1: Info, 2: Error
	VerifySSL                 bool   `env:"VERIFY_SSL" envDefault:"true"` // Controls SSL certificate verification
}
