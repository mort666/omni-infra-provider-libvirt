package cloudinit

// MetaData is a struct to render the meta data of the cloud init configuration
type MetaData struct {
	Raw           map[string]any `json:"-" yaml:"-"`
	LocalHostname string         `json:"local-hostname,omitempty" yaml:"local-hostname,omitempty"`
	InstanceID    string         `json:"instance-id,omitempty" yaml:"instance-id,omitempty"`
	Hostname      string         `json:"hostname,omitempty" yaml:"hostname,omitempty"`
}

func (md *MetaData) Marshal() ([]byte, error) {
	return mergeMarshal(md, md.Raw)
}

func (md *MetaData) Unmarshal(data []byte) error {
	return rawUnmarshal(data, md, &md.Raw)
}

func (md *MetaData) Merge(md2 *MetaData) error {
	return merge(md, md2)
}
