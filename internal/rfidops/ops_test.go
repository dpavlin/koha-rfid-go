package rfidops

import "testing"

type programStub struct {
	writes int
}

func (p *programStub) Inventory() ([]string, error)                        { return nil, nil }
func (p *programStub) InventoryWithReset() ([]string, error)               { return nil, nil }
func (p *programStub) ReadBlocks(string, int, int) (map[int]string, error) { return nil, nil }
func (p *programStub) ReadAfi(string) (byte, error)                        { return 0, nil }
func (p *programStub) WriteBlocks(string, string) error                    { p.writes++; return nil }
func (p *programStub) WriteAfi(string, byte) error                         { p.writes++; return nil }
func (p *programStub) Lock()                                               {}
func (p *programStub) Unlock()                                             {}

func TestProgramRejectsContentOverRFID501Limit(t *testing.T) {
	ops := &programStub{}
	result := Program(ops, []ProgramOp{{SID: "E2001234567890AB", Content: "12345678901234567"}})
	if result.OK != 0 || len(result.Errors) != 1 {
		t.Fatalf("result = %#v, want one validation error", result)
	}
	if ops.writes != 0 {
		t.Fatalf("writes = %d, want no tag writes", ops.writes)
	}
}
