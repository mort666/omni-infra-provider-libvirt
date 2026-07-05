package libvirt

import (
	"fmt"
	"os"

	"libvirt.org/go/libvirtxml"

	"github.com/mort666/omni-infra-provider-libvirt/internal/pkg/libvirt/cloudinit"
	"github.com/mort666/omni-infra-provider-libvirt/internal/pkg/libvirt/image"
	"github.com/mort666/omni-infra-provider-libvirt/internal/pkg/libvirt/vm"
	"github.com/mort666/omni-infra-provider-libvirt/internal/pkg/libvirt/volume"
)

type Manager struct {
	ConfigImagePool string
	BaseImagePool   string
	VMImagePool     string
	BridgeCount     uint64
	Image           image.Manager
	Volume          volume.Manager
	VM              vm.Manager
}

func (m *Manager) Create(name, baseImageName string, vmConfig *vm.Config, ciConfig *cloudinit.Config) error {
	baseImage, err := m.Image.Get(m.BaseImagePool, baseImageName)
	if err != nil {
		return fmt.Errorf("failed to get image: %w", err)
	}

	image, err := m.Image.Clone(baseImage.ID, m.VMImagePool, name, vmConfig.DiskSize)
	if err != nil {
		return fmt.Errorf("failed to clone image '%s': %w", baseImage, err)
	}
	vmConfig.Image = image.ID

	volName := fmt.Sprintf("user-data-%s.iso", image.ID)

	isoConfig, err := ciConfig.ISO(volName)

	if err != nil {
		return fmt.Errorf("failed to create config ISO: %w", err)
	}

	fh, err := os.Open(isoConfig)
	if err != nil {
		return fmt.Errorf("error opening local disk image: %w", err)
	}
	defer fh.Close()

	isoImage, err := m.Image.Create(m.ConfigImagePool, volName, fh)
	if err != nil {
		// try to cleanup cloned base image
		_ = m.Image.Remove(image.ID)
		return fmt.Errorf("failed to store config ISO: %w", err)
	}
	vmConfig.ISO = isoImage.Name

	disks := []libvirtxml.DomainDisk{
		{
			Device: "disk",
			Driver: &libvirtxml.DomainDiskDriver{
				Name:  "qemu",
				Type:  "raw",
				Cache: "none",
				IO:    "native",
			},
			Target: &libvirtxml.DomainDiskTarget{
				Dev: "sda",
				Bus: "scsi",
			},
			Source: &libvirtxml.DomainDiskSource{
				File: &libvirtxml.DomainDiskSourceFile{
					File: vmConfig.Image,
				},
			},
		}, {
			Device: "cdrom",
			Driver: &libvirtxml.DomainDiskDriver{
				Name: "qemu",
				Type: "raw",
			},
			Target: &libvirtxml.DomainDiskTarget{
				Dev: "sdb",
				Bus: "sata",
			},
			Source: &libvirtxml.DomainDiskSource{
				File: &libvirtxml.DomainDiskSourceFile{
					File: vmConfig.ISO,
				},
			},
		},
	}
	networks := []libvirtxml.DomainInterface{
		{
			Source: &libvirtxml.DomainInterfaceSource{
				Network: &libvirtxml.DomainInterfaceSourceNetwork{
					Network: vmConfig.Network,
				},
			},
			Model: &libvirtxml.DomainInterfaceModel{
				Type: "virtio",
			},
		},
	}

	err = m.VM.Create(name, vmConfig, disks, networks)
	if err != nil {
		return err
	}
	return nil
}

func (m *Manager) Remove(name string) error {
	state, err := m.VM.Get(name)
	if err != nil {
		return err
	}

	for _, imageID := range state.Images {
		err := m.Image.Remove(imageID)
		if err != nil {
			return err
		}
	}

	return m.VM.Remove(name)
}
