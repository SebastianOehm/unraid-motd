package datasources

import "time"

// ConfBase is the common type for all modules
//
// Custom modules should respect these options
type ConfBase struct {
	// Override global setting
	WarnOnly        *bool `yaml:"warnings_only,omitempty"`
	FixedTableWidth *int  `yaml:"table_width,omitempty"`
	Border          *bool `yaml:"border,omitempty"`
	Timeout         *int  `yaml:"timeout,omitempty"`
}

func (c *ConfBase) Init() {
}

func (c ConfBase) GetBorder() bool {
	return *c.Border
}

func (c ConfBase) GetTableWidth() int {
	return *c.FixedTableWidth
}

// GetTimeout returns the module timeout, 0 means no limit
func (c ConfBase) GetTimeout() time.Duration {
	if c.Timeout == nil || *c.Timeout <= 0 {
		return 0
	}

	return time.Duration(*c.Timeout) * time.Second
}

func (c *ConfBase) Load(conf *Conf) {
	if c.FixedTableWidth == nil {
		c.FixedTableWidth = &conf.FixedTableWidth
	}
	if c.WarnOnly == nil {
		c.WarnOnly = &conf.WarnOnly
	}
	if c.Border == nil {
		c.Border = &conf.Border
	}
	if c.Timeout == nil {
		c.Timeout = &conf.Timeout
	}
}
