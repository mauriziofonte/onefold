# Translating onefold

The GUI speaks English, Italian, Spanish, French, German and Brazilian Portuguese. The command line and the reports stay in English.

Every language is one flat JSON file in [`internal/gui/web/src/lib/locales/`](internal/gui/web/src/lib/locales/): the key is the place in the GUI, the value is the text. `en.json` is the source: a key that a translation does not have shows the English text.

## What a translation must keep

`TestLocales` (`internal/gui/locales_test.go`) checks every file on every pull request. A translation:

- has exactly the keys of `en.json`, no more and no fewer;
- keeps every placeholder in braces, such as `{n}`, `{size}` or `{month-name}`, spelled as in English. Their order in the sentence may change;
- keeps the same HTML tags (`<b>`, `<p>`, `<p class="text-rose-700">`, `<span class="font-mono">`, `<br>`...). The words inside the tags are translated. Path notation such as `PICTURES/<year>/` is plain text and may be translated;
- is not empty where the English text is not.

Keep the names of commands, flags, presets and file formats as they are (`onefold catalog`, `-execute`, `photo-month`, `media`, `JPEG`). Keep "golden" for the golden archive, as the English and Italian texts do.

## On Weblate

The project is prepared for [Hosted Weblate](https://hosted.weblate.org/), which hosts libre projects free of charge after its maintainers approve the request. The project has not been created there yet: the owner of the repository does that once, with the settings below. Until then, translations come as pull requests that change the JSON files directly.

The `.weblate` file at the root of the repository lets the [`wlc`](https://docs.weblate.org/en/latest/wlc.html) client find the component once it exists.

| Setting | Value |
|---|---|
| Project | `onefold`, website https://github.com/mauriziofonte/onefold |
| Component | `gui` |
| Source code repository | `https://github.com/mauriziofonte/onefold.git`, branch `main` |
| Version control system | GitHub pull request (Weblate pushes to its own branch and opens a pull request) |
| File format | JSON file |
| File mask | `internal/gui/web/src/lib/locales/*.json` |
| Monolingual base language file | `internal/gui/web/src/lib/locales/en.json` |
| Template for new translations | `internal/gui/web/src/lib/locales/en.json` |
| Edit base file | off (English changes come with the code that needs them) |
| Language code style | BCP style using a hyphen (`pt-BR`) |
| Adding new translation | Contact maintainers (a new language also needs a line in `internal/gui/lang.go` and `internal/gui/web/src/lib/i18n.svelte.js`) |
| Translation flags | `placeholders:r"\{[a-z][a-zA-Z0-9-]*\}", safe-html` |
| Translation license | GNU Affero General Public License v3.0 or later |
| Contributor agreement | "Translations are contributions: before your first translation is merged, sign the onefold Individual CLA once (CLA.md in the repository)." |

Every pull request from Weblate runs the same CI as any other, `TestLocales` included, and goes through the [Contributor License Agreement](CLA.md) check like every contribution.

## Adding a language

Open an issue first. A new language needs:

1. a new `internal/gui/web/src/lib/locales/<code>.json` with every key of `en.json`;
2. the code in `Langs` in `internal/gui/lang.go` (the server picks it from the browser's `Accept-Language`) and in `LANGS`, `LANG_NAMES` and `FORMATS` in `internal/gui/web/src/lib/i18n.svelte.js`;
3. `make web`, then `go test ./internal/gui`.
