package forgejo

import "testing"

// TestForgeLabelDefaultAndOverride covers the forgeLabel accessor used by
// user-named aliases. The default must equal the driver name so existing
// call sites that never call SetForgeLabel behave exactly as before; an
// override (e.g. "gitea") must win.
func TestForgeLabelDefaultAndOverride(t *testing.T) {
	c := &Client{}
	if got := c.forgeLabelOrDefault(); got != "forgejo" {
		t.Errorf("default forgeLabel = %q, want %q", got, "forgejo")
	}
	c.SetForgeLabel("gitea")
	if got := c.forgeLabelOrDefault(); got != "gitea" {
		t.Errorf("overridden forgeLabel = %q, want %q", got, "gitea")
	}
}

// TestSetForgeLabelIgnoresEmpty guards against SetForgeLabel("") clobbering
// the default — an empty override should be a no-op so a caller that has
// not decided on a label yet cannot accidentally stamp the empty string on
// every returned resource.
func TestSetForgeLabelIgnoresEmpty(t *testing.T) {
	c := &Client{}
	c.SetForgeLabel("")
	if got := c.forgeLabelOrDefault(); got != "forgejo" {
		t.Errorf("empty override must be ignored, got %q", got)
	}
}
