# Omni Infrastructure Provider for libvirt

## Fork and Differences vs Upstream

Fork of original omni-infra-provider-libvirt provider to change the vm creation to build more robust VM and use OpenVSwitch infrastructure on the VM hypervisor to handle networking that is less brittle and has fewer restrictions for connectivity.

- Use of nocloud ISO and startup userdata creation reworked to be a little more forgiving and stop images using external DNS resolvers during initial startup which results in a startup loop that cannot locate locally hosted Omni servers when local DNS naming is not externally managed.
- OpenVSwitch is used on the hypervisor to handle VM connectivity to the host and wider LAN environments, using bridged network devices that avoid libvirtd limitations for bridged network access. Somewhat hacky code as target devices must be unique and provisioning process for libvirtd is opaque and none intuitive for detection of existing devices with a given name.
- Deprovision is a little more structured to handle UEFI NVRAM, deletion of storage etc. So previous implementation would fail on VM creation due to NVRAM varstore not being properly undefined and storage removed.
- ISOs used for startup not the qcow2 disk image in similar way to Proxmox provider as it is more reliable, with unique ISO per VM to mitigate volume sharing issues.
- VM initial disk is created blank to make it less likely to end up in a boot loop provisioning with bad network and nameserver setup as qcow2 image seems to use hard coded external DNS
- VM creation management code is very much WIP but works and reliably provisions and deprovisions multiple VMs whereas previous version would often fail with duplicate volumes and network devices

This is not supported by siderolabs, and this initial vision is a little 'hacky'.

## Overview

Can be used to automatically provision Talos nodes in `libvirtd`.

## Configuration

In your Omni instance under Settings -> Infra Providers, create a new `libvirt` provider.
Make a note of the `OMNI_ENDPOINT` and `OMNI_SERVICE_ACCOUNT_KEY`.

We now show various ways to connect to libvirt.
For more options see the [libvirt URI docs](https://libvirt.org/uri.html)

### Connecting to libvirt via ssh

You must ensure to mount the proper SSH keys.
This also requires the SSH user to have access to libvirt on the server side.

Create the configuration file for the provider:

```yaml
libvirt:
  uri: 'qemu+libssh://user@hostname/system?known_hosts_verify=ignore'
```

### Connecting to libvirt via socket

If using Docker, this requires to mount the libvirt socket into the container.

```yaml
libvirt:
  uri: 'qemu:///system'
  # If using libvirt via Homebrew on MacOS:
  # url: 'qemu:///session?socket=/Users/<username>/.cache/libvirt/libvirt-sock'
```

## Running the provider

> **_NOTE:_**
> `omni-infra-provider-libvirt` will not create storage pools nor networks.
> It will optimistically assume that they already exist and are functional.

### Using Docker

Copy the provider credentials created in omni to an `.env` file

```env
# your omni instance URL
OMNI_ENDPOINT=https://<OMNI_INSTANCE_NAME>.<REGION>.omni.siderolabs.io
# base64 encoded key as shown by omni
OMNI_SERVICE_ACCOUNT_KEY=<PROVIDER_KEY>
```

Example for using the above `ssh` based connection method:

```shell
docker run \
  --name omni-infra-provider-libvirt \
  --rm -it \
  -e USER=$USER \
  --env-file /tmp/omni-provider-libvirt.env \
  -v /tmp/omni-provider-libvirt.yaml:/config.yaml \
  -v /home/user/.ssh:/.ssh:ro \
  ghcr.io/siderolabs/omni-infra-provider-libvirt \
    --config-file /config.yaml
```

Example for using the above `socket` based connection method:

> **_NOTE:_**
> don't blindly copy-paste this, the location might vary depending on your linux distribution.
> ensure the socket actually exists on your host at the given path.

```shell
docker run \
  --name omni-infra-provider-libvirt \
  --rm -it \
  -e USER=$USER \
  --env-file /tmp/omni-provider-libvirt.env \
  -v /tmp/omni-provider-libvirt.yaml:/config.yaml \
  -v /var/run/libvirt/libvirt-sock:/var/run/libvirt/libvirt-sock:rw \
  ghcr.io/siderolabs/omni-infra-provider-libvirt \
    --config-file /config.yaml
```

## How to use in an Omni cluster template

See [test/](./test/) for some examples

## Development

See `make help` for general build info.

Build an image:

```shell
make generate image-omni-infra-provider-libvirt-linux-amd64
```

Build the binary:

```shell
# e.g. for darwin
make omni-infra-provider-libvirt-darwin-arm64
```

Run the linter:

```shell
make lint-fmt fmt
```
