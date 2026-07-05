package volume

import (
	"io"

	"github.com/digitalocean/go-libvirt"
)

type Manager interface {
	Create(pool, name, format string, capacity uint64) (*Volume, error) 
	CreateFrom(pool, name, format string, capacity uint64, image io.ReadCloser) (*Volume, error) 
	Remove(name string) error
	List(pool string) ([]Volume, error)
	Get(pool, name string) (*Volume, error)
}

type Volume struct {
	ID   string
	Name string
	Pool string
	Volume libvirt.StorageVol
}
