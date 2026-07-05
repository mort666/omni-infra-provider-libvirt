package vm

import (
	"libvirt.org/go/libvirtxml"
)

type Manager interface {
	Create(name string, config *Config, disks []libvirtxml.DomainDisk, networks []libvirtxml.DomainInterface) error
	Start(name string) error
	Shutdown(name string, force bool) error
	Remove(name string) error
	List() ([]VM, error)
	Get(name string) (*VM, error)
}

type Config struct {
	Image             string
	ISO               string
	Memory            uint64
	CPUCount          uint
	Network           string
	DiskSize          uint64
	Uuid              string
}

type VM struct {
	Name      string
	State     string
	IPAddress string
	Images    []string
}
