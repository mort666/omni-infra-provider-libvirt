// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

// Package provider implements libvirt infra provider core.
package provider

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/digitalocean/go-libvirt"
	"github.com/google/uuid"
	"github.com/siderolabs/omni/client/pkg/constants"
	"github.com/siderolabs/omni/client/pkg/infra/provision"
	"go.uber.org/zap"
	"libvirt.org/go/libvirtxml"

	"github.com/mort666/omni-infra-provider-libvirt/api/specs"
	libvirtmanager "github.com/mort666/omni-infra-provider-libvirt/internal/pkg/libvirt"
	"github.com/mort666/omni-infra-provider-libvirt/internal/pkg/libvirt/cloudinit"
	vmmanager "github.com/mort666/omni-infra-provider-libvirt/internal/pkg/libvirt/vm"
	"github.com/mort666/omni-infra-provider-libvirt/internal/pkg/provider/resources"
)

const (
	MiB             = uint64(1024 * 1024)
	GiB             = MiB * 1024
	diskFormatQcow2 = "qcow2"
	diskFormatRaw   = "raw"
)

// Provisioner implements Talos emulator infra provider.
type Provisioner struct {
	libvirtClient *libvirt.Libvirt
	imageCache    *ImageCache
	libvirtmgr    *libvirtmanager.Manager
}

// NewProvisioner creates a new provisioner.
func NewProvisioner(libvirtClient *libvirt.Libvirt, imageCache *ImageCache, manager *libvirtmanager.Manager) *Provisioner {
	return &Provisioner{
		libvirtClient: libvirtClient,
		imageCache:    imageCache,
		libvirtmgr:    manager,
	}
}

var errUploadImage = errors.New("error uploading image")

// ProvisionSteps implements infra.Provisioner.
//
//nolint:gocognit,gocyclo,cyclop,maintidx
func (p *Provisioner) ProvisionSteps() []provision.Step[*resources.Machine] {
	return []provision.Step[*resources.Machine]{
		provision.NewStep(
			"generateUUID",
			func(ctx context.Context, logger *zap.Logger, pctx provision.Context[*resources.Machine]) error {
				newUUID := uuid.New()

				dom, err := p.libvirtClient.DomainLookupByUUID(libvirt.UUID(newUUID))
				if err != nil {
					if dom.UUID != libvirt.UUID(newUUID) {
						// found unused UUID
						pctx.State.TypedSpec().Value.Uuid = newUUID.String()
						pctx.SetMachineUUID(pctx.State.TypedSpec().Value.Uuid)

						return nil
					}

					return provision.NewRetryError(err, time.Second*10)
				}

				return provision.NewRetryInterval(time.Second * 1)
			},
		),

		provision.NewStep(
			"createSchematic",
			func(ctx context.Context, logger *zap.Logger, pctx provision.Context[*resources.Machine]) error {

				schematicID, err := pctx.GenerateSchematicID(
					ctx, logger,
					provision.WithExtraExtensions("siderolabs/qemu-guest-agent", "siderolabs/glibc", "siderolabs/fuse3", "siderolabs/util-linux-tools", "siderolabs/iscsi-tools"),
					provision.WithoutConnectionParams(),
					provision.WithExtraKernelArgs(),
				)
				if err != nil {
					return provision.NewRetryErrorf(time.Second*10, "error generating schematic ID: %w", err)
				}

				logger.Info("created schematic " + schematicID)
				pctx.State.TypedSpec().Value.Schematic = schematicID
				return nil
			},
		),

		provision.NewStep(
			"provisionISO",
			func(ctx context.Context, logger *zap.Logger, pctx provision.Context[*resources.Machine]) error {
				var data Data

				err := pctx.UnmarshalProviderData(&data)
				if err != nil {
					return err
				}

				url, err := url.Parse(constants.ImageFactoryBaseURL)
				if err != nil {
					return err
				}
				vmName := pctx.GetRequestID()
				url = url.JoinPath(
					"image",
					pctx.State.TypedSpec().Value.Schematic,
					pctx.GetTalosVersion(),
					"nocloud-amd64.iso",
				)

				// Acquire image from cache (downloads if needed, deduplicates concurrent requests)
				filePath, err := p.imageCache.Acquire(ctx, pctx.State.TypedSpec().Value.Schematic, pctx.GetTalosVersion(), "iso")
				if err != nil {
					return provision.NewRetryErrorf(time.Second*10, "error fetching image: %w", err)
				}
				defer p.imageCache.Release(pctx.State.TypedSpec().Value.Schematic, pctx.GetTalosVersion(), "iso")

				fi, _ := os.Stat(filePath)

				logger.Info("acquired iso", zap.String("local-cache", filePath), zap.Uint64("file-size", uint64(fi.Size())))

				isoName := fmt.Sprintf("%s-%s-nocloud-amd64.iso", vmName, pctx.State.TypedSpec().Value.Schematic)

				logger.Info("provisioning iso", zap.String("nocloud-amd64-iso", url.String()))

				fh, err := os.Open(filePath)
				if err != nil {
					return fmt.Errorf("error opening local disk image: %w", err)
				}
				defer fh.Close()

				// if volume exists, delete old version
				if vol, errGetVol := getVol(p.libvirtClient, "isos", isoName); errGetVol == nil {
					if errVolDel := p.libvirtClient.StorageVolDelete(vol, 0); errVolDel != nil {
						return fmt.Errorf("delete old cidata volume: %w, name: %s", errVolDel, isoName)
					}
				}

				vol, err := p.libvirtmgr.Volume.CreateFrom("isos", isoName, diskFormatRaw, uint64(fi.Size()), fh)
				if err != nil {
					return err
				}
				pctx.State.TypedSpec().Value.IsoVolName = vol.Name
				pctx.State.TypedSpec().Value.IsoVolPath = vol.ID

				logger.Info("provisioned ISO", zap.String("volume", vol.Name), zap.String("local-path", vol.ID))
				return nil
			},
		),

		provision.NewStep(
			"provisionVMVolume",
			func(ctx context.Context, logger *zap.Logger, pctx provision.Context[*resources.Machine]) error {
				var data Data

				err := pctx.UnmarshalProviderData(&data)
				if err != nil {
					return err
				}

				vmName := pctx.GetRequestID()
				volName := fmt.Sprintf("%s-volume", vmName)

				vol, err := p.libvirtmgr.Volume.Create(data.StoragePool, volName, diskFormatRaw, data.DiskSize*GiB)
				if err != nil {
					return err
				}

				pctx.State.TypedSpec().Value.PoolName = vol.Pool
				pctx.State.TypedSpec().Value.VmVolName = vol.Name
				pctx.State.TypedSpec().Value.VmVolPath = vol.ID

				return nil
			},
		),

		provision.NewStep(
			"provisionAdditionalDisks",
			func(ctx context.Context, logger *zap.Logger, pctx provision.Context[*resources.Machine]) error {
				var data Data

				err := pctx.UnmarshalProviderData(&data)
				if err != nil {
					return err
				}

				if len(data.AdditionalDisks) > 0 {
					var (
						additionalDisks []*specs.AdditionalDisk
						vmName          = pctx.GetRequestID()
					)

					for idx, additionalDiskSpec := range data.AdditionalDisks {
						volName := fmt.Sprintf("%s-%d-%s", vmName, idx, additionalDiskSpec.Type)
						volSize := additionalDiskSpec.Size * GiB

						_, err = createVolume(p.libvirtClient, data.StoragePool, volName, additionalDiskSpec.Type, volSize)
						if err != nil {
							return fmt.Errorf("error creating disk: %w", err)
						}

						additionalDisks = append(
							additionalDisks,
							&specs.AdditionalDisk{
								Type:    additionalDiskSpec.Type,
								VolName: volName,
							},
						)
					}

					pctx.State.TypedSpec().Value.AdditionalDisks = additionalDisks
				}

				logger.Info("provisioned additional disks", zap.Int("count", len(data.AdditionalDisks)))

				return nil
			},
		),

		provision.NewStep(
			"provisionCidata",
			func(ctx context.Context, logger *zap.Logger, pctx provision.Context[*resources.Machine]) error {
				// create CIDATA for nocloud, contains the hostname
				// docs: https://docs.siderolabs.com/talos/latest/platform-specific-installations/cloud-platforms/nocloud#cdrom%2Fusb
				var data Data

				err := pctx.UnmarshalProviderData(&data)
				if err != nil {
					return err
				}

				var (
					vmName  = pctx.GetRequestID()
					volName = fmt.Sprintf("user-data-%s.iso", vmName)
				)

				metadata := &cloudinit.MetaData{
					InstanceID:    pctx.State.TypedSpec().Value.Uuid,
					LocalHostname: vmName,
					Hostname:      vmName,
				}

				networkdata := &cloudinit.NetworkConfig{
					Version: 2,
					Ethernets: map[string]cloudinit.Ethernet{
						"all-en": {
							Match: cloudinit.Match{
								Name: "en*",
							},
							DHCP:  true,
							DHCP6: true,
							DNS: cloudinit.DNS{
								Servers: []string{"10.131.69.164", "10.131.69.128"},
							},
						},
						"all-eth": {
							Match: cloudinit.Match{
								Name: "eth*",
							},
							DHCP:  true,
							DHCP6: true,
							DNS: cloudinit.DNS{
								Servers: []string{"10.131.69.164", "10.131.69.128"},
							},
						},
					},
				}

				vendordata := pctx.ConnectionParams.JoinConfig
				logger.Info("cloud-init", zap.Any("metadata", metadata), zap.String("userdata", pctx.ConnectionParams.JoinConfig), zap.Any("networkdata", networkdata))

				if vol, errGetVol := getVol(p.libvirtClient, "config", volName); errGetVol == nil {
					if errVolDel := p.libvirtClient.StorageVolDelete(vol, 0); errVolDel != nil {
						return fmt.Errorf("delete old cidata volume: %w, name: %s", errVolDel, volName)
					}
				}

				talosconfig := cloudinit.NewTalosConfig(nil, metadata, &vendordata, networkdata, logger)

				isoConfig, err := talosconfig.ISO(volName)

				if err != nil {
					return fmt.Errorf("failed to create config ISO: %w", err)
				}

				fi, _ := os.Stat(isoConfig)

				logger.Info("acquired iso", zap.String("local-cache", isoConfig), zap.Uint64("file-size", uint64(fi.Size())))

				logger.Info("provisioning cidata ISO", zap.String("volume", volName))

				fh, err := os.Open(isoConfig)
				if err != nil {
					return fmt.Errorf("error opening local disk image: %w", err)
				}
				defer fh.Close()

				isoImage, err := p.libvirtmgr.Volume.CreateFrom("config", volName, diskFormatRaw, uint64(fi.Size()), fh)
				if err != nil {
					return err
				}

				pctx.State.TypedSpec().Value.CidataVolName = isoImage.Name
				pctx.State.TypedSpec().Value.CidataVolPath = isoImage.ID

				logger.Info("provisioned cidata ISO", zap.String("volume", isoImage.Name), zap.String("local-path", isoImage.ID))

				return nil
			},
		),

		provision.NewStep(
			"createVM",
			func(ctx context.Context, logger *zap.Logger, pctx provision.Context[*resources.Machine]) error {
				volName := pctx.State.TypedSpec().Value.VmVolName
				if volName == "" {
					return provision.NewRetryErrorf(time.Second*10, "waiting for image")
				}

				var data Data

				err := pctx.UnmarshalProviderData(&data)
				if err != nil {
					return err
				}

				vmName := pctx.GetRequestID()

				// assemble primary disk volume

				_, err = getVol(p.libvirtClient, data.StoragePool, volName)
				if err != nil {
					return provision.NewRetryErrorf(time.Second*10, "error fetching volume: %w", err)
				}

				disks := []libvirtxml.DomainDisk{
					{
						Device: "disk",
						Boot: &libvirtxml.DomainDeviceBoot{
							Order: 1,
						},
						Driver: &libvirtxml.DomainDiskDriver{
							Name:  "qemu",
							Type:  "raw",
							Cache: "none",
							IO:    "native",
						},
						Alias: &libvirtxml.DomainAlias{
							Name: "scsi0-0-0-3",
						},
						Address: &libvirtxml.DomainAddress{
							Drive: &libvirtxml.DomainAddressDrive{
								Controller: Pointer(uint(0)),
								Bus:        Pointer(uint(0)),
								Target:     Pointer(uint(0)),
								Unit:       Pointer(uint(3)),
							},
						},
						Target: &libvirtxml.DomainDiskTarget{
							Dev: "sdd",
							Bus: "scsi",
						},
						Source: &libvirtxml.DomainDiskSource{
							File: &libvirtxml.DomainDiskSourceFile{
								File: pctx.State.TypedSpec().Value.VmVolPath,
							},
						},
					}, {
						Device: "cdrom",
						Boot: &libvirtxml.DomainDeviceBoot{
							Order: 2,
						},
						ReadOnly: &libvirtxml.DomainDiskReadOnly{},
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
								File: pctx.State.TypedSpec().Value.IsoVolPath,
							},
						},
					}, {
						Device: "cdrom",
						Boot: &libvirtxml.DomainDeviceBoot{
							Order: 3,
						},
						Driver: &libvirtxml.DomainDiskDriver{
							Name: "qemu",
							Type: "raw",
						},
						Target: &libvirtxml.DomainDiskTarget{
							Dev: "sdc",
							Bus: "sata",
						},
						Source: &libvirtxml.DomainDiskSource{
							File: &libvirtxml.DomainDiskSourceFile{
								File: pctx.State.TypedSpec().Value.CidataVolPath,
							},
						},
					},
				}

				// assemble additional disk volumes

				var (
					sataDiskCount = 2 // account for root disk
					nvmeDiskCount = 0
				)

				for _, additionalDisk := range pctx.State.TypedSpec().Value.AdditionalDisks {
					var dev, bus string

					switch additionalDisk.Type {
					case "nvme":
						{
							dev = fmt.Sprintf("nvme%dn1", nvmeDiskCount)
							bus = "nvme"
							nvmeDiskCount++
						}
					case "sata":
						{
							idx := sataDiskCount

							s := ""
							for idx >= 0 {
								s = fmt.Sprint(rune('a'+(idx%26))) + s
								idx = idx/26 - 1
							}

							dev = fmt.Sprintf("sd%s", s)
							bus = "virtio"
							sataDiskCount++
						}
					default:
						{
							return fmt.Errorf("unknown disk type: %q", additionalDisk.Type)
						}
					}

					additionalDisk := libvirtxml.DomainDisk{
						Device: "disk",
						Driver: &libvirtxml.DomainDiskDriver{
							Name:  "qemu",
							Type:  "qcow2",
							Cache: "none",
							IO:    "native",
						},
						Source: &libvirtxml.DomainDiskSource{
							Volume: &libvirtxml.DomainDiskSourceVolume{
								Pool:   data.StoragePool,
								Volume: additionalDisk.VolName,
							},
						},
						Target: &libvirtxml.DomainDiskTarget{
							Dev: dev,
							Bus: bus,
						},
						Serial: uuid.NewString(),
					}

					disks = append(disks, additionalDisk)
				}

				// assemble network interfaces

				// <interface type="bridge" trustGuestRxFilters="yes">
				//   <mac address="52:54:00:ab:ff:ff"/>
				//   <source bridge="ovsbr0"/>
				//   <virtualport type="openvswitch">
				//     <parameters interfaceid="58893bf5-fc72-408e-970f-7cb8023357ed"/>
				//   </virtualport>
				//   <target dev="vmbr0"/>
				//   <model type="virtio"/>
				//   <alias name="net0"/>
				//   <address type="pci" domain="0x0000" bus="0x07" slot="0x00" function="0x0"/>
				// </interface>

				var networkInterfaces []libvirtxml.DomainInterface

				ifnum := p.libvirtmgr.BridgeCount
				var ifname string

				for _, ifaceData := range data.NetworkInterfaces {
					if ifaceData.TargetName != "" && strings.Compare(ifaceData.TargetName, "omnibr") == 0 {
						ifname = fmt.Sprintf("%s%d", ifaceData.TargetName, ifnum)
						p.libvirtmgr.BridgeCount = p.libvirtmgr.BridgeCount + 1
						pctx.State.TypedSpec().Value.VmIfName = ifname
					}

					iface := libvirtxml.DomainInterface{
						MAC: &libvirtxml.DomainInterfaceMAC{
							Address: Default(ifaceData.PhysicalAddress, MacSingle()),
						},
						Target: &libvirtxml.DomainInterfaceTarget{
							Dev: Default(pctx.State.TypedSpec().Value.VmIfName, ifname, "omnibr0"),
						},
						TrustGuestRXFilters: "yes",
						Model: &libvirtxml.DomainInterfaceModel{
							Type: Default(ifaceData.Driver, "virtio"),
						},
						VirtualPort: &libvirtxml.DomainInterfaceVirtualPort{
							Params: &libvirtxml.DomainInterfaceVirtualPortParams{
								OpenVSwitch: &libvirtxml.DomainInterfaceVirtualPortParamsOpenVSwitch{},
							},
						},
						Source: &libvirtxml.DomainInterfaceSource{
							Bridge: &libvirtxml.DomainInterfaceSourceBridge{
								Bridge: Default(ifaceData.BridgeName, "ovsbr0"),
							},
						},
					}

					networkInterfaces = append(networkInterfaces, iface)
				}

				vmCfg := vmmanager.Config{
					Uuid:     pctx.State.TypedSpec().Value.Uuid,
					CPUCount: data.Cores,
					Memory:   uint64(data.Memory),
				}
				err = p.libvirtmgr.VM.Create(vmName, &vmCfg, disks, networkInterfaces)
				if err != nil {
					return fmt.Errorf("creating domain: %w", err)
				}


				// set VM id in omni
				pctx.State.TypedSpec().Value.VmName = vmName
				pctx.State.TypedSpec().Value.VmNvramName = fmt.Sprintf("%s_VARS.fd", vmName)

				return nil
			},
		),

		provision.NewStep(
			"startVM",
			func(ctx context.Context, logger *zap.Logger, pctx provision.Context[*resources.Machine]) error {
				vmName := pctx.State.TypedSpec().Value.VmName

				err := p.libvirtmgr.VM.Start(vmName)

				if err != nil {
					if !strings.Contains(err.Error(), "domain is already running") {
						return provision.NewRetryErrorf(time.Second*10, "failed to start VM: %w", err)
					}
				}

				return nil
			},
		),
	}
}
