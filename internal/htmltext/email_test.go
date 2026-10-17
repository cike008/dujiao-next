package htmltext

import "testing"

func TestToEmailText(t *testing.T) {
	input := `<section><h3>Service</h3><p>
    Copy your <strong>Key</strong> first.
    Then continue.
  </p><a href="https://example.test/terminal?a=1&amp;b=2">Open terminal</a>
  <p>Support: Moshshop <button onclick="copy()">Copy ID</button></p>
  <script>secretScript()</script><style>.x{}</style><a href="javascript:alert(1)">Unsafe</a></section>`
	want := "Service\nCopy your Key first. Then continue.\n\nOpen terminal (https://example.test/terminal?a=1&b=2)\nSupport: Moshshop\n\nUnsafe"
	if got := ToEmailText(input); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestToEmailTextPreservesPlainTextLines(t *testing.T) {
	input := "  First step\n  Second step\n\n Support: demo\u00a0ID\n"
	want := "First step\nSecond step\n\nSupport: demo ID"
	if got := ToEmailText(input); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestToEmailTextPreservesPreformattedInstructions(t *testing.T) {
	input := "<p>Configuration:</p><pre><code>KEY=demo\nREGION=us\n  indented: true\n\tcommand --flag</code></pre><p>Next step</p>"
	want := "Configuration:\n\nKEY=demo\nREGION=us\n  indented: true\n\tcommand --flag\nNext step"
	if got := ToEmailText(input); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
