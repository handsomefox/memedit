package winmem

import "testing"

func TestScannable(t *testing.T) {
	tests := []struct {
		name          string
		state         uint32
		protect       uint32
		typ           uint32
		includeMapped bool
		want          bool
	}{
		{"committed readwrite private", memCommit, pageReadwrite, 0, false, true},
		{"committed writecopy", memCommit, pageWritecopy, 0, false, true},
		{"committed exec-readwrite", memCommit, pageExecuteReadwrite, 0, false, true},
		{"committed exec-writecopy", memCommit, pageExecuteWritecopy, 0, false, true},
		{"reserved not committed", 0x2000, pageReadwrite, 0, false, false},
		{"read-only not writable", memCommit, 0x02 /*PAGE_READONLY*/, 0, false, false},
		{"no-access", memCommit, 0x01 /*PAGE_NOACCESS*/, 0, false, false},
		{"guard page skipped", memCommit, pageReadwrite | pageGuard, 0, false, false},
		{"mapped excluded by default", memCommit, pageReadwrite, memMapped, false, false},
		{"mapped included when asked", memCommit, pageReadwrite, memMapped, true, true},
		{"writable with nocache modifier", memCommit, pageReadwrite | 0x200 /*PAGE_NOCACHE*/, 0, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := scannable(tt.state, tt.protect, tt.typ, tt.includeMapped); got != tt.want {
				t.Errorf("scannable(state=%#x protect=%#x typ=%#x mapped=%v) = %v, want %v",
					tt.state, tt.protect, tt.typ, tt.includeMapped, got, tt.want)
			}
		})
	}
}
