package clipboard

import "testing"

func TestLinkHTML(t *testing.T) {
	got := linkHTML("https://github.com/owner/repo/issues/1?a=1&b=2", `Fix "thing" <now>`)
	want := `<a href="https://github.com/owner/repo/issues/1?a=1&amp;b=2">Fix &#34;thing&#34; &lt;now&gt;</a>`
	if got != want {
		t.Fatalf("linkHTML() = %q, want %q", got, want)
	}
}
