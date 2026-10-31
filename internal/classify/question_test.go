package classify

import "testing"

func TestEndsWithQuestion(t *testing.T) {
	cases := []struct {
		name string
		msg  string
		want bool
	}{
		{"plain question", "I updated the parser.\n\nWant me to add tests?", true},
		{"bold question", "Summary.\n\n**Should I proceed?**", true},
		{"parenthesised", "Done (shall I also update docs?)", true},
		{"full-width", "完了しました。続けますか？", true},
		{"question in middle line of last paragraph", "Options:\nShould I use A or B?\n- A is faster", true},
		{"question only in code block", "Here is the snippet:\n\n```\nif ok?\n```", false},
		{"question only in earlier paragraph", "Should I proceed?\n\nI went ahead and did it.", false},
		{"question before trailing code block", "Should I run this?\n```sh\nmake\n```", true},
		{"statement", "All tests pass.", false},
		{"empty", "", false},
		{"imperative request", "Let me know which.", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := EndsWithQuestion(c.msg); got != c.want {
				t.Fatalf("EndsWithQuestion(%q) = %v, want %v", c.msg, got, c.want)
			}
		})
	}
}
