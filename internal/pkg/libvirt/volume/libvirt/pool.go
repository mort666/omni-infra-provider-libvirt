package libvirt

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/digitalocean/go-libvirt"
	"libvirt.org/go/libvirtxml"

	"github.com/mort666/omni-infra-provider-libvirt/internal/pkg/libvirt/volume"
)

var _ volume.Manager = &Manager{}

type Manager struct {
	name string
	pool string
	*libvirt.Libvirt
}

var (
	errCreateVol  = errors.New("error creating volume")
	errVolNoExist = errors.New("volume does not exist")
)

func New(name string, pool string, libvirt *libvirt.Libvirt) *Manager {
	return &Manager{
		name,
		pool,
		libvirt,
	}
}

func (m *Manager) Create(pool, name, format string, capacity uint64) (*volume.Volume, error) {
	if vol, err := m.getVol(pool, name); err == nil {
		location, err := m.StorageVolGetPath(vol)
		if err != nil {
			return nil, err
		}
		return &volume.Volume{
			Name:   vol.Name,
			Pool:   vol.Pool,
			ID:     location,
			Volume: vol,
		}, nil
	}

	var vol libvirt.StorageVol

	storagepool, err := m.StoragePoolLookupByName(pool)
	if err != nil {
		return &volume.Volume{
			Name:   vol.Name,
			Pool:   vol.Pool,
			Volume: vol,
		}, fmt.Errorf("%w: %w", errCreateVol, err)
	}

	volData := libvirtxml.StorageVolume{
		Type: "file",
		Name: name,
		Allocation: &libvirtxml.StorageVolumeSize{
			// thin provision: allocate zero bytes at time of creation
			Unit:  "bytes",
			Value: 0,
		},
		Capacity: &libvirtxml.StorageVolumeSize{
			Unit:  "bytes",
			Value: capacity,
		},
		Target: &libvirtxml.StorageVolumeTarget{
			Format: &libvirtxml.StorageVolumeTargetFormat{
				Type: format,
			},
		},
	}

	volXML, err := volData.Marshal()
	if err != nil {
		return &volume.Volume{
			Name:   vol.Name,
			Pool:   vol.Pool,
			Volume: vol,
		}, fmt.Errorf("%w, error rendering XML: %w", errCreateVol, err)
	}

	vol, err = m.StorageVolCreateXML(storagepool, volXML, 0)
	if err != nil {
		return &volume.Volume{
			Name:   vol.Name,
			Pool:   vol.Pool,
			Volume: vol,
		}, fmt.Errorf("%w: error creating volume: %w", errCreateVol, err)
	}

	location, err := m.StorageVolGetPath(vol)
	if err != nil {
		return nil, err
	}

	volume := &volume.Volume{
		Name:   vol.Name,
		Pool:   vol.Pool,
		ID:     location,
		Volume: vol,
	}

	return volume, nil
}

func (m *Manager) List(pool string) ([]volume.Volume, error) {
	sp, err := m.createOrGetPool(pool)
	if err != nil {
		return nil, fmt.Errorf("faild to get storage pool: %s", err)
	}

	vols, _, err := m.StoragePoolListAllVolumes(*sp, 1, 0)
	if err != nil {
		return nil, err
	}

	volumes := []volume.Volume{}
	for _, vol := range vols {
		location, err := m.StorageVolGetPath(vol)
		if err != nil {
			return nil, err
		}
		volumes = append(volumes, volume.Volume{
			ID:   location,
			Name: vol.Name,
			Pool: vol.Pool,
		})
	}
	return volumes, nil
}

func (m *Manager) Remove(ID string) error {
	vol, err := m.StorageVolLookupByPath(ID)
	if err != nil {
		return fmt.Errorf("faild to get storage pool: %s", err)
	}
	return m.StorageVolDelete(vol, 0)
}

func (m *Manager) Get(pool, name string) (*volume.Volume, error) {
	sp, err := m.createOrGetPool(pool)
	if err != nil {
		return nil, fmt.Errorf("faild to get storage pool: %w", err)
	}

	sv, err := m.StorageVolLookupByName(*sp, name)
	if err != nil {
		return nil, err
	}

	location, err := m.StorageVolGetPath(sv)
	if err != nil {
		return nil, err
	}
	return &volume.Volume{
		Name: sv.Name,
		Pool: sv.Pool,
		ID:   location,
	}, nil
}

func (m *Manager) CreateFrom(pool, name, format string, capacity uint64, img io.ReadCloser) (*volume.Volume, error) {

	vol := &libvirtxml.StorageVolume{
		Name: name,
		Capacity: &libvirtxml.StorageVolumeSize{
			Value: 0,
		},
		Target: &libvirtxml.StorageVolumeTarget{
			Permissions: &libvirtxml.StorageVolumeTargetPermissions{
				Mode: "0444",
			},
		},
	}

	xml, err := vol.Marshal()
	if err != nil {
		return nil, err
	}

	sp, err := m.createOrGetPool(pool)
	if err != nil {
		return nil, fmt.Errorf("faild to get storage pool: %w", err)
	}

	sv, err := m.StorageVolCreateXML(*sp, xml, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to create volume: %w", err)
	}

	err = m.StorageVolUpload(sv, img, 0, 0, 0)
	if err != nil {
		// try undo
		_ = m.StorageVolDelete(sv, 0)
		return nil, fmt.Errorf("failed to upload content: %w", err)
	}

	location, err := m.StorageVolGetPath(sv)
	if err != nil {
		return nil, fmt.Errorf("failed to get volume location: %w", err)
	}
	return &volume.Volume{
		Name: sv.Name,
		Pool: sv.Pool,
		ID:   location,
	}, nil
}

func (m *Manager) getVol(poolName, volName string) (libvirt.StorageVol, error) {
	var vol libvirt.StorageVol

	pool, err := m.StoragePoolLookupByName(poolName)
	if err != nil {
		return vol, fmt.Errorf("getvol: %w", err)
	}

	vol, err = m.StorageVolLookupByName(pool, volName)
	if err != nil {
		// TODO: there is probably a better way to check this
		if strings.Contains(err.Error(), "Storage volume not found") {
			return vol, errVolNoExist
		}

		return vol, err
	}

	return vol, nil
}

func (m *Manager) createOrGetPool(pool string) (*libvirt.StoragePool, error) {
	sp, err := m.StoragePoolLookupByName(pool)
	if err == nil {
		return &sp, nil
	}

	// TODO: seems that the underlaying errors of libvirt are not exported
	if !strings.Contains(err.Error(), "Storage pool not found") {
		return nil, err
	}

	storagePool := libvirtxml.StoragePool{
		Type: "dir",
		Name: pool,
		Target: &libvirtxml.StoragePoolTarget{
			Path: filepath.Join(m.name, pool),
		},
	}

	xml, err := storagePool.Marshal()
	if err != nil {
		return nil, err
	}
	sp, err = m.StoragePoolCreateXML(xml, libvirt.StoragePoolCreateWithBuild)
	if err != nil {
		return nil, fmt.Errorf("failed to create storage pool: %w", err)
	}
	return &sp, nil
}
