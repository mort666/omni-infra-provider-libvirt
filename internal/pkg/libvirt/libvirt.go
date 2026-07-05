package libvirt

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"time"

	"github.com/digitalocean/go-libvirt"
	"github.com/spf13/cobra"
	"go.uber.org/zap"

	image "github.com/mort666/omni-infra-provider-libvirt/internal/pkg/libvirt/image/libvirt"
	vm "github.com/mort666/omni-infra-provider-libvirt/internal/pkg/libvirt/vm/libvirt"
	volume "github.com/mort666/omni-infra-provider-libvirt/internal/pkg/libvirt/volume/libvirt"
)

type LibvirtOptions struct {
	URI          string
	BaseImageDir string
}

func (o *LibvirtOptions) BindFlags(cmd *cobra.Command, prefix string) {
	cmd.Flags().StringVar(&o.URI, prefix+"uri", o.URI, "URI to connecto to libvirtd. either a unix socket in the format unix:/socket/path or an IP in the format tcp:127.0.0.1.")
	cmd.Flags().StringVar(&o.BaseImageDir, prefix+"image-base-dir", o.BaseImageDir, "Base directory to create new storage pools for the images.")
}

func NewLibvirtDefaultOptions() *LibvirtOptions {
	return &LibvirtOptions{
		URI:          "unix:/var/run/libvirt/libvirt-sock",
		BaseImageDir: "/var/lib/libvirt/images/vu",
	}
}

func NewLibvirtManager(o *LibvirtOptions, logger *zap.Logger) (*Manager, error) {
	libvirtConn, err := connectLibvirt(o.URI, logger)
	if err != nil {
		return nil, err
	}
	return &Manager{
		ConfigImagePool: "config",
		BaseImagePool:   "isos",
		VMImagePool:     "vm",
		Image:           image.New(o.BaseImageDir, libvirtConn),
		VM:              vm.New(libvirtConn, logger),
		Volume:          volume.New("", "", libvirtConn),
	}, nil
}

func connectLibvirt(uri string, logger *zap.Logger) (*libvirt.Libvirt, error) {

	url, _ := url.Parse(uri)

	libvirtClient, err := libvirt.ConnectToURI(url)
	if err != nil {
		return nil, fmt.Errorf("error connecting to libvirt: %w", err)
	}

	if !libvirtClient.IsConnected() {
		return nil, errors.New("client is not connected")
	}

	ver, err := libvirtClient.ConnectGetVersion()
	if err != nil {
		return nil, fmt.Errorf("error fetching version: %w", err)
	} else {
		logger.Info(fmt.Sprintf("libvirtVersion: %d", ver))
	}

	return libvirtClient, nil
}

type dialer struct {
	network string
	address string
	timeout time.Duration
}

func newDialer(network, address string, timeout time.Duration) *dialer {
	return &dialer{
		network: network,
		address: address,
		timeout: timeout,
	}
}

func (d *dialer) Dial() (net.Conn, error) {
	return net.DialTimeout(d.network, d.address, d.timeout)
}
