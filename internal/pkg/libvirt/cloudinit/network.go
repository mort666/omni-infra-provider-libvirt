package cloudinit

import (
	"net"

	"github.com/goccy/go-yaml"
)

type NetworkConfig struct {
	Raw       map[string]any      `json:"-" yaml:"-"`
	Version   int                 `json:"version" yaml:"version,omitempty"`
	Ethernets map[string]Ethernet `json:"ethernets,omitempty" yaml:"ethernets,omitempty"`
	Configs   []NetConfig         `json:"config,omitempty" yaml:"config,omitempty"`
}

type NetConfig struct {
	Type       string   `json:"type,omitempty" yaml:"type,omitempty"`
	Name       string   `json:"name,omitempty" yaml:"name,omitempty"`
	MacAddress string   `json:"mac_address,omitempty" yaml:"mac_address,omitempty"`
	Subnets    []Subnet `json:"subnets,omitempty" yaml:"subnets,omitempty"`
}

type Subnet struct {
	Type    string `json:"type,omitempty" yaml:"type,omitempty"`
	Address string `json:"address,omitempty" yaml:"address,omitempty"`
	Gateway string `json:"gateway,omitempty" yaml:"gateway,omitempty"`
	Netmask string `json:"netmask,omitempty" yaml:"netmask,omitempty"`
}

type Ethernet struct {
	Match     Match    `json:"match,omitempty" yaml:"match,omitempty"`
	Addresses []string `json:"addresses,omitempty" yaml:"addresses,omitempty"`
	DHCP      bool     `json:"dhcp4,omitempty" yaml:"dhcp4,omitempty"`
	DHCP6     bool     `json:"dhcp6,omitempty" yaml:"dhcp6,omitempty"`
	Gateway   string   `json:"gateway4,omitempty" yaml:"gateway4,omitempty"`
	Gateway6  string   `json:"gateway6,omitempty" yaml:"gateway6,omitempty"`
	DNS       DNS      `json:"nameservers,omitempty" yaml:"dns,omitempty"`
}

type Match struct {
	Driver string `json:"driver,omitempty" yaml:"driver,omitempty"`
	Name   string `json:"name,omitempty" yaml:"name,omitempty"`
	MAC    string `json:"macaddress,omitempty" yaml:"macaddress,omitempty"`
}

type DNS struct {
	Servers []string `json:"addresses,omitempty" yaml:"addresses,omitempty"`
	Search  []string `json:"search,omitempty" yaml:"search,omitempty"`
}

func (nc *NetworkConfig) Marshal() ([]byte, error) {
	return yaml.Marshal(nc)
}

func (nc *NetworkConfig) Unmarshal(data []byte) error {
	return yaml.Unmarshal(data, nc)
}

func (nc *NetworkConfig) Merge(nc2 *NetworkConfig) error {
	return merge(nc, nc2)
}

type NetworkConfigOptions struct {
	Address    string
	Gateway    string
	Nameserver []string
}

func NewNetworkConfig(nco NetworkConfigOptions) (*NetworkConfig, error) {
	var (
		// will only work for one interface
		matchName  = "en*"
		gateway    string
		nameserver = []string{}
	)

	if nco.Address == "" {
		return nil, nil
	}

	_, ipNet, err := net.ParseCIDR(nco.Address)
	if err != nil {
		return nil, err
	}

	if nco.Gateway == "" {
		gateway = getGatewayIP(ipNet).String()
	} else {
		gateway = nco.Gateway
	}

	if len(nco.Nameserver) == 0 {
		nameserver = append(nameserver, gateway)
	} else {
		nameserver = nco.Nameserver
	}

	c := &NetworkConfig{
		Version: 2,
		Ethernets: map[string]Ethernet{
			"default": {
				Match: Match{
					Name: matchName,
				},
				Addresses: []string{nco.Address},
				Gateway:   gateway,
				DNS: DNS{
					Servers: nameserver,
				},
			},
		},
	}
	return c, nil
}

func getGatewayIP(ipNet *net.IPNet) net.IP {
	return incrementIP(ipNet.IP, 1)
}

// incrementIP increments an IP https://stackoverflow.com/a/49057611
func incrementIP(ip net.IP, inc uint) net.IP {
	i := ip.To4()
	v := uint(i[0])<<24 + uint(i[1])<<16 + uint(i[2])<<8 + uint(i[3])
	v += inc
	v3 := byte(v & 0xFF)
	v2 := byte((v >> 8) & 0xFF)
	v1 := byte((v >> 16) & 0xFF)
	v0 := byte((v >> 24) & 0xFF)
	return net.IPv4(v0, v1, v2, v3)
}
