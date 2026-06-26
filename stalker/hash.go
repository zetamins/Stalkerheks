package stalker

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
)

// hashVersion1Salt is a fixed constant baked into stbapp's native code,
// found in .rodata as 8 concatenated ASCII fragments. Not derived from any
// input.
const hashVersion1Salt = "dA0j6HpVFcMgNjUBDr0QhwTBIzLHDIrynuQy4XNJ"

func sha1Hex(s string) string {
	h := sha1.Sum([]byte(s))
	return hex.EncodeToString(h[:])
}

// hashVersion1 emulates the real STB's native GetHashVersion1(data1, data2)
// JS-bridge method. Verified by dynamically executing the real function
// (Unicorn ARM emulation of stbapp's native code, with QString/QByteArray
// ABI-compliant inputs and resolved PLT stubs) against 6 independent test
// vectors, all matching exactly:
//
//	h1 := sha1(data1)
//	h2 := sha1(h1 + hashVersion1Salt)
//	result := sha1(h2 + data2)
//
// There is a separate, MD5-based code path taken only when data2 is exactly
// 56 bytes long (confirmed structurally via NEON MD5 constant matching in
// static disassembly, but its exact byte layout couldn't be dynamically
// verified — a VFP/NEON instruction crashed the emulator). None of our
// data2 values (handshake random, version string) are ever exactly 56
// bytes, so this always takes the verified general-case path.
func hashVersion1(data1, data2 string) string {
	h1 := sha1Hex(data1)
	h2 := sha1Hex(h1 + hashVersion1Salt)
	return sha1Hex(h2 + data2)
}

// Metrics builds the JSON metrics blob real STBs send: mac, sn, model,
// type and a persistent device uid.
func (p *Portal) Metrics() string {
	m := map[string]string{
		"mac":   p.MAC,
		"sn":    p.SerialNumber,
		"model": p.Model,
		"type":  "STB",
		"uid":   p.DeviceID2,
	}
	b, _ := json.Marshal(m)
	return string(b)
}

// Prehash computes the "prehash" get_profile parameter as
// GetHashVersion1(model, version).
func (p *Portal) Prehash() string {
	return hashVersion1(p.Model, p.VersionString())
}

// HWVersion2 computes the "hw_version_2" get_profile parameter as
// GetHashVersion1(metrics, random) — random being the value the portal
// returned in this Portal's own handshake response.
func (p *Portal) HWVersion2() string {
	return hashVersion1(p.Metrics(), p.Random)
}
