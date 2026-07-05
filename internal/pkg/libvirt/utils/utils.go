package utils

import (
	"errors"
	"strings"

	"github.com/digitalocean/go-libvirt"
	"go.uber.org/zap"
)

func CheckDomainInterfaceByName(conn *libvirt.Libvirt, logger *zap.Logger, vmname, name string) (*libvirt.DomainInterface, error) {
	var value libvirt.DomainInterface

	n, err := conn.ConnectNumOfDomains()
	if err != nil {
		return nil, err
	}

	doms, _, err := conn.ConnectListAllDomains(n, libvirt.ConnectListDomainsRunning)
	if err != nil {
		return nil, err
	}

	for _, dom := range doms {
		logger.Info("checking for domain interfaces", zap.String("domain", dom.Name), zap.String("vmname", vmname))
		ifaces, err := GetAllInterfaces(conn, &dom)
		if err != nil {
			if libvirt.IsNotFound(err) || CheckError(err, libvirt.ErrOperationInvalid) {
				logger.Error("Invalid Domain for interface", zap.String("domain", dom.Name), zap.String("vmname", vmname))
				continue
			} else {
				return nil, err
			}
		}

		for _, iface := range ifaces {
			logger.Info("checking domain interface", zap.String("ifname", name), zap.String("domain", dom.Name), zap.String("iface", iface.Name), zap.String("vmname", vmname))
			if strings.Compare(iface.Name, name) == 0 {
				value = iface
				return &value, nil
			}
		}

	}

	return nil, nil
}

func GetAllInterfaces(conn *libvirt.Libvirt, domain *libvirt.Domain) ([]libvirt.DomainInterface, error) {
	var ifaces []libvirt.DomainInterface

	domifaces, err := conn.DomainInterfaceAddresses(*domain, uint32(libvirt.DomainInterfaceAddressesSrcLease), 0)
	if err != nil {
		return nil, err
	}
	ifaces = append(ifaces, domifaces...)

	domifaces, err = conn.DomainInterfaceAddresses(*domain, uint32(libvirt.DomainInterfaceAddressesSrcAgent), 0)
	if err != nil {
		return ifaces, err
	}

	ifaces = append(ifaces, domifaces...)

	domifaces, err = conn.DomainInterfaceAddresses(*domain, uint32(libvirt.DomainInterfaceAddressesSrcArp), 0)
	if err != nil {
		return ifaces, err
	}

	ifaces = append(ifaces, domifaces...)

	return ifaces, nil
}

func CheckError(err error, expectedError libvirt.ErrorNumber) bool {
	for err != nil {
		e, ok := err.(libvirt.Error)
		if ok {
			return e.Code == uint32(expectedError)
		}
		err = errors.Unwrap(err)
	}
	return false
}
