## Twitch Social Bot v2.1.0

### What's New / Novedades

- **Global `SOUL.md` & Unfiltered Banter / Alma Global y Sin Autocensura**
  - Added global `SOUL.md` and `SOUL.es.md` root directives so every persona naturally talks like a real Twitch chat regular without corporate filler.
  - Explicitly allows casual swearing and strong slang in character, and cleared restrictive per-persona `blocked_words` defaults.

- **Modular Character Architecture & Local Vector Memory / Memoria Vectorial Local**
  - Added modular `bio`, `lore`, `adjectives`, `topics`, `style`, and few-shot `message_examples` support to YAML personas.
  - Raised viewer memory capacity from `15` to **`150` facts** and added a zero-dependency local N-gram cosine similarity search that retrieves the **5 most relevant + 3 latest facts** per turn.
  - Made `tiers` and `slots` 100% optional for organic custom or imported personalities.

- **Universal Character Card Importer (`.png` / `.json` / `.charx`) / Importador Universal de Tarjetas**
  - Added **`↓ Import Character Card`** in the CLI: drag & drop community V1/V2/V3 `.png` cards (`ccv3` & `chara` chunks), `.charx` archives, or `.json` files.
  - Automatically extracts the embedded avatar to Unicode Braille, strips roleplay asterisk narration, and creates a ready-to-use persona.

- **Configurable `18+` Filter (`ALLOW_NSFW`) / Filtro `+18` Configurable**
  - Added `ALLOW_NSFW` (default `false` safe mode for Twitch) with an arrow-key toggle inside the CLI setup wizard.

---

### How to Update / Cómo Actualizar
- **From v1.x / v2.0.0:** Open `twitch-social-bot.exe` and press **Enter** on **Update now and restart**—your `.env`, `.tokens.json`, and `data/` memory are preserved automatically.
- **New users:** Download `twitch-social-bot-windows.zip` below, extract it, and double-click `twitch-social-bot.exe`.
