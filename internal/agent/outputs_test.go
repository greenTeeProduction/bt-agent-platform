package agent

import "testing"

func TestValidateOutputs_Empty(t *testing.T) {
	specs := []OutputSpec{{Name: "report", Type: "markdown"}}
	ok, reasons := ValidateOutputs(specs, "   ")
	if ok || len(reasons) == 0 {
		t.Fatalf("expected empty output failure, ok=%v reasons=%v", ok, reasons)
	}
}

func TestValidateOutputs_Markdown(t *testing.T) {
	specs := []OutputSpec{{Name: "report", Type: "markdown"}}
	ok, reasons := ValidateOutputs(specs, "## Report\n\nAll good.")
	if !ok || len(reasons) > 0 {
		t.Fatalf("expected markdown pass, ok=%v reasons=%v", ok, reasons)
	}
}

func TestValidateOutputs_JSONDirect(t *testing.T) {
	specs := []OutputSpec{{Name: "result", Type: "json"}}
	ok, reasons := ValidateOutputs(specs, `{"status":"ok"}`)
	if !ok || len(reasons) > 0 {
		t.Fatalf("expected json pass, ok=%v reasons=%v", ok, reasons)
	}
}

func TestValidateOutputs_JSONFence(t *testing.T) {
	specs := []OutputSpec{{Name: "result", Type: "json"}}
	out := "Here is the report:\n```json\n{\"routed\":true,\"delivered\":true}\n```"
	ok, reasons := ValidateOutputs(specs, out)
	if !ok || len(reasons) > 0 {
		t.Fatalf("expected fenced json pass, ok=%v reasons=%v", ok, reasons)
	}
}

func TestValidateOutputs_JSONInvalid(t *testing.T) {
	specs := []OutputSpec{{Name: "result", Type: "json"}}
	ok, reasons := ValidateOutputs(specs, "not json at all")
	if ok {
		t.Fatal("expected json failure")
	}
	if len(reasons) == 0 {
		t.Fatal("expected reasons")
	}
}

func TestValidateOutputs_NoSpec(t *testing.T) {
	ok, reasons := ValidateOutputs(nil, "")
	if !ok || len(reasons) > 0 {
		t.Fatalf("expected pass with no spec, ok=%v reasons=%v", ok, reasons)
	}
}
