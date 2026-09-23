package uilanguage

import "testing"

func TestLanguageChoice(t *testing.T) {
	for _, example := range []struct{ input, want string }{
		{"zh-Hans-US", Chinese}, {"zh-Hant-TW", Chinese}, {"zh_CN", Chinese},
		{"en-US", English}, {"ja-JP", English}, {"fr-FR", English},
	} {
		if got := FromLanguage(example.input); got != example.want {
			t.Fatalf("FromLanguage(%q) = %q, want %q", example.input, got, example.want)
		}
	}
	if got := firstAppleLanguage("(\n    \"zh-Hans-US\",\n    \"en-US\"\n)"); got != "zh-Hans-US" {
		t.Fatalf("first language = %q", got)
	}
	if Mode("") != Auto || Mode("unknown") != Auto || !Valid(Auto) || Valid("fr") {
		t.Fatal("invalid preference handling")
	}
}
