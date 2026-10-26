package native

import (
	"bytes"
	"testing"
)

func TestARM32EHABIFrameOperationBound(t *testing.T) {
	crash, err := ParseMinidump(buildARM32Minidump(368))
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		length int
		want   bool
	}{
		{length: 256, want: true},
		{length: 257, want: false},
	} {
		program := bytes.Repeat([]byte{0x00}, test.length)
		_, _, _, ok := unwindARM32EHABI(program, crash.registers, crash.stack)
		if ok != test.want {
			t.Fatalf("%d EHABI operations ok=%v want=%v", test.length, ok, test.want)
		}
	}
}

func TestARM32DefaultCFIRegisters(t *testing.T) {
	initial := cfiState{regs: map[uint32]cfiRegisterRule{
		0: {kind: uint8(cfiRuleRegister), reg: 1},
		9: {kind: uint8(cfiRuleOffset), value: -4},
	}}
	got := arm32DefaultCFIRegisters(initial)
	if got.regs[0].kind != uint8(cfiRuleRegister) || got.regs[0].reg != 1 {
		t.Fatalf("explicit r0 rule was replaced: %+v", got.regs[0])
	}
	if got.regs[9].kind != uint8(cfiRuleOffset) || got.regs[9].value != -4 {
		t.Fatalf("explicit r9 rule was replaced: %+v", got.regs[9])
	}
	for register := uint32(0); register < 16; register++ {
		if register == 0 || register == 9 {
			continue
		}
		want := cfiRuleUndefined
		switch register {
		case 4, 5, 6, 7, 8, 10, 11, 13:
			want = cfiRuleSame
		}
		if got.regs[register].kind != uint8(want) {
			t.Fatalf("r%d default=%d want=%d", register, got.regs[register].kind, want)
		}
	}
}
