//go:build !windows

package agent

import "testing"

func TestCPUModel(t *testing.T) {
	x86 := "processor\t: 0\nvendor_id\t: GenuineIntel\nmodel name\t: Intel(R) Xeon(R) CPU E5-2680 v4 @ 2.40GHz\n\nprocessor\t: 1\nmodel name\t: Other\n"
	pi4 := "processor\t: 0\nBogoMIPS\t: 108.00\nFeatures\t: fp asimd evtstrm crc32 cpuid\nCPU implementer\t: 0x41\nCPU part\t: 0xd08\nCPU revision\t: 3\n\n" +
		"processor\t: 1\nBogoMIPS\t: 108.00\n\nHardware\t: BCM2835\nRevision\t: c03115\nSerial\t\t: 10000000deadbeef\nModel\t\t: Raspberry Pi 4 Model B Rev 1.5\n"
	oldPi := "processor\t: 0\nmodel name\t: ARMv7 Processor rev 3 (v7l)\nHardware\t: BCM2835\nModel\t\t: Raspberry Pi 3 Model B\n"
	hwOnly := "processor\t: 0\nCPU part\t: 0xd03\nHardware\t: Rockchip RK3399\n"
	bare := "processor\t: 0\nBogoMIPS\t: 50.00\nCPU part\t: 0xd0c\n"
	cases := []struct{ name, cpuinfo, dt, want string }{
		{"x86 takes the first model name", x86, "", "Intel(R) Xeon(R) CPU E5-2680 v4 @ 2.40GHz"},
		{"Raspberry Pi 4 (arm64) names the board, not the SoC", pi4, "Raspberry Pi 4 Model B Rev 1.5\x00", "Raspberry Pi 4 Model B Rev 1.5"},
		{"a model name still wins on ARM", oldPi, "", "ARMv7 Processor rev 3 (v7l)"},
		{"Hardware when there is no Model", hwOnly, "", "Rockchip RK3399"},
		{"device tree when cpuinfo says nothing", bare, "Some Board\x00", "Some Board"},
		{"nothing known", bare, "", ""},
		{"no cpuinfo at all", "", "", ""},
	}
	for _, c := range cases {
		if got := cpuModel(c.cpuinfo, c.dt); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}
