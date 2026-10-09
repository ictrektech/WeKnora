package types

import "testing"

func TestNormalizeSupportedLocale(t *testing.T) {
	for _, locale := range []string{"zh-CN", "zh-TW", "en-US", "ko-KR", "ja-JP", "ru-RU"} {
		if got := NormalizeSupportedLocale(locale); got != locale {
			t.Errorf("NormalizeSupportedLocale(%q) = %q", locale, got)
		}
	}
	if got := NormalizeSupportedLocale("fr-FR"); got != "" {
		t.Errorf("NormalizeSupportedLocale(unsupported) = %q, want empty", got)
	}
}

func TestNormalizeEmbedDefaultLocaleTraditionalChinese(t *testing.T) {
	if got := NormalizeEmbedDefaultLocale("zh-TW"); got != "zh-TW" {
		t.Errorf("NormalizeEmbedDefaultLocale(zh-TW) = %q, want zh-TW", got)
	}
	if got := NormalizeEmbedDefaultLocale("zh-Hant"); got != "" {
		t.Errorf("NormalizeEmbedDefaultLocale(zh-Hant) = %q, want empty unsupported alias", got)
	}
}
