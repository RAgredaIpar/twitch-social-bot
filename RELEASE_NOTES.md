## Twitch Social Bot v2.0.0

### What's New / Novedades

- **Multi-AI Provider Support & Live Model Discovery / Soporte Multi-IA**
  - Choose between **DeepSeek**, **Claude (Anthropic)**, **ChatGPT (OpenAI)**, **Google Gemini**, **Groq**, **Grok (xAI)**, or any **Custom / Private API** (OpenRouter, Ollama, vLLM).
  - Automatically queries `GET /models` using your API key during setup to select the latest chat model—zero manual model names required.
  - CLI header now displays **Model** (in each provider's brand color) and **API Key** status separately.

- **Per-Persona Braille Avatars & Theme Colors / Avatares Braille y Colores por Personalidad**
  - Each persona now renders its own crisp `28×12` (`56×48` subpixels) Unicode Braille Twitch icon and custom CLI frame color:
    - **Mochi** (`cohost_amigos`): Emerald Green (`#34D399`)
    - **Celeste** (`drama_romance`): Tsundere Pink (`#F472B6`) with vivid blush accents
    - **Lyra** (`rpg_facciones`): Gamer Cyan (`#38BDF8`) with RPG circlet
    - **Dante** (`clan_jerarquia`): Clan Red (`#EF4444`) with sunglasses & cigar
  - **Create New Persona (`+ Create New Persona`)**: Build new personalities right inside the CLI, pick a theme color, and drag-and-drop any `.png` / `.jpg` / `.gif` image to convert it into Braille art automatically.

- **More Human-Like Chat Engine / Comportamiento Más Humano**
  - Variable turn pacing & minimal 1–4 word replies for ultra-short messages (`"hola"`, `"xd"`, `"f"`).
  - Anti-repetition penalties, decisive opinionated stance, anti-AI-cliché rules, dynamic session mood (`chill`, `hype`, `annoyed`, `playful`), Twitch badge awareness (`Broadcaster`, `Mod`, `VIP`, `Sub`, `New`), and live stream game/title context.

- **Updated Documentation & Banner / Documentación Actualizada**
  - Refreshed `README.md` and `README.es.md` with the new project banner and full v2.0.0 guide.

---

### How to Update / Cómo Actualizar
- **From v1.0.0 / v1.0.1:** Simply open `twitch-social-bot.exe` and press **Enter** on **Update now and restart**—your `.env`, `.tokens.json`, and `data/` memory are preserved automatically.
- **New users:** Download `twitch-social-bot-windows.zip` below, extract it, and double-click `twitch-social-bot.exe`.
