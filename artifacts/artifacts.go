package artifacts

import (
	"embed"
	"io/fs"
)

const (
	Station1Firmware        = "firmware/station-1/yandexstation-2.304.5.15.3194115689.20250703.52.zip"
	Station1FirmwareSHA256  = "05b9bd8f9cfadcfdd23a6e55bc941ab6e4822ff23a1138f6c31c2685ff10c9c1"
	Station1FirmwareVersion = "2.304.5.15.3194115689.20250703.52"
	Station1FactoryVersion  = "1.3.7.0.631346810.20200316.factory"
	Station1Boot            = "boot/station-1/candidate.img"
	Station1StockBoot       = "boot/station-1/stock-reference.img"
	Station1NoTelnetRamdisk = "boot/station-1/no-telnet.cpio.gz"
)

// Only public binaries and images belong here. Keys and device backups never do.
// The firmware stays in a read-only filesystem: serving it does not copy 663 MiB.
//
//go:embed firmware/station-1/*.zip boot/station-1/* ssh/station-1/*
var files embed.FS

func FS() fs.FS { return files }

func ReadFile(name string) ([]byte, error) { return files.ReadFile(name) }
