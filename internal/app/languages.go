package app

// modelLanguages maps the BCP 47 codes stored in preferences to the English
// names used in model prompts. Keep it in sync with frontend/src/languages.ts.
var modelLanguages = map[string]string{
	"zh-Hans": "Simplified Chinese",
	"zh-Hant": "Traditional Chinese",
	"en":      "English",
	"ja":      "Japanese",
	"ko":      "Korean",
	"fr":      "French",
	"de":      "German",
	"es":      "Spanish",
	"pt":      "Portuguese",
	"it":      "Italian",
	"ru":      "Russian",
	"ar":      "Arabic",
	"hi":      "Hindi",
	"th":      "Thai",
	"vi":      "Vietnamese",
	"id":      "Indonesian",
	"tr":      "Turkish",
	"nl":      "Dutch",
	"pl":      "Polish",
	"sv":      "Swedish",
}

// modelLanguageName resolves a target language code for prompts.
func modelLanguageName(code string) (string, bool) {
	name, ok := modelLanguages[code]
	return name, ok
}