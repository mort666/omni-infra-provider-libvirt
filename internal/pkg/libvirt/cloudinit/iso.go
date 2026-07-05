package cloudinit

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/diskfs/go-diskfs/backend/file"
	"github.com/diskfs/go-diskfs/filesystem/iso9660"
)

const (
	volumeIdentifier = "cidata"
	blockSize        = 2048
	DefaultCachePath = "/tmp/omni-libvirt-config-cache"
)

// ISO returns the cloud init configuration as ISO image
func (c *Config) ISO(filename string) (string, error) {
	var (
		meta    []byte
		user    []byte
		network []byte
		err     error
		buf     bytes.Buffer
	)
	if c.MetaData != nil {
		meta, err = c.MetaData.Marshal()
		if err != nil {
			return "", fmt.Errorf("could not render meta data: %s", err)
		}
		buf.WriteString("### meta-data ###\n")
		buf.Write(meta)
		buf.WriteString("\n")
	}

	if c.UserData != nil && !c.TalosConfig {
		user, err = c.UserData.Marshal()
		if err != nil {
			return "", fmt.Errorf("could not render user data: %s", err)
		}
		buf.WriteString("### user-data ###\n")
		buf.Write(user)
		buf.WriteString("\n")
	}

	if c.TalosConfig {
		user = []byte(c.VendorData)
		buf.WriteString("### user-data ###\n")
		buf.Write(user)
		buf.WriteString("\n")
	}

	if c.NetworkConfig != nil {
		network, err = c.NetworkConfig.Marshal()
		if err != nil {
			return "", fmt.Errorf("could not render network config: %s", err)
		}
		buf.WriteString("### network-config ###\n")
		buf.Write(network)
		buf.WriteString("\n")
	}

	isoimage, err := makeCloudInitISO(filename, user, meta, []byte{}, network)
	return isoimage, err
}

func marshalToIso(fs *iso9660.FileSystem, file string, m Marshaler) error {
	if m == nil {
		return nil
	}
	data, err := m.Marshal()
	if err != nil {
		return err
	}
	return addFile(fs, file, data)
}

func addFile(fs *iso9660.FileSystem, name string, content []byte) error {
	rw, err := fs.OpenFile("/"+name, os.O_CREATE|os.O_RDWR)
	if err != nil {
		return err
	}

	if _, err = rw.Write(content); err != nil {
		return err
	}

	// Joliet finalization in go-diskfs v1.9+ requires the file handle to
	// be closed before Finalize so its size is recorded correctly.
	if err = rw.Close(); err != nil {
		return err
	}

	return nil
}

func makeCloudInitISO(filename string, userdata, metadata, vendordata, networkconfig []byte) (isopath string, err error) {
	isopath = filepath.Join(DefaultCachePath, filename)

	isoFile, err := os.Create(isopath)
	if err != nil {
		return "", err
	}

	if err := isoFile.Close(); err != nil {
		return "", err
	}

	iso, err := file.OpenFromPath(isopath, false)
	if err != nil {
		return "", err
	}

	defer func() {
		if cerr := iso.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	fs, err := iso9660.Create(iso, 0, 0, blockSize, "")
	if err != nil {
		return "", err
	}

	if err = fs.Mkdir("/"); err != nil {
		return "", err
	}

	cifiles := map[string][]byte{
		"user-data": userdata,
		"meta-data": metadata,
	}

	if len(vendordata) != 0 {
		cifiles["vendor-data"] = vendordata
	}

	if len(networkconfig) != 0 {
		cifiles["network-config"] = networkconfig
	}

	for name, content := range cifiles {
		rw, err := fs.OpenFile("/"+name, os.O_CREATE|os.O_RDWR)
		if err != nil {
			return "", err
		}

		if _, err = rw.Write(content); err != nil {
			return "", err
		}

		// Joliet finalization in go-diskfs v1.9+ requires the file handle to
		// be closed before Finalize so its size is recorded correctly.
		if err = rw.Close(); err != nil {
			return "", err
		}
	}

	if err = fs.Finalize(iso9660.FinalizeOptions{
		RockRidge:        true,
		Joliet:           true,
		VolumeIdentifier: volumeIdentifier,
	}); err != nil {
		return "", err
	}

	return
}
