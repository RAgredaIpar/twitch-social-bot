package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/twitch-social-bot/internal/cli"
	"github.com/twitch-social-bot/internal/config"
	"github.com/twitch-social-bot/internal/domain"
	"github.com/twitch-social-bot/internal/engine"
	"github.com/twitch-social-bot/internal/i18n"
	"github.com/twitch-social-bot/internal/llm"
	"github.com/twitch-social-bot/internal/persona"
	"github.com/twitch-social-bot/internal/ratelimit"
	"github.com/twitch-social-bot/internal/room"
	"github.com/twitch-social-bot/internal/safety"
	"github.com/twitch-social-bot/internal/server"
	"github.com/twitch-social-bot/internal/social"
	"github.com/twitch-social-bot/internal/store"
	"github.com/twitch-social-bot/internal/twitch"
	"github.com/twitch-social-bot/internal/updater"
)

// Version can be overridden at build time via -ldflags "-X main.Version=vX.Y.Z".
var Version = "v1.0.0"

type stdoutSender struct{}

func (s *stdoutSender) SendReply(ctx context.Context, broadcasterID, senderID, replyParentID, text string) error {
	log.Printf("[CHAT OUT] (replyTo: %s): %s", replyParentID, text)
	return nil
}

func main() {
	updater.CleanupOldBinary()

	envFile := flag.String("env", ".env", "Path to .env configuration file")
	headless := flag.Bool("run", false, "Start bot directly without interactive CLI menu")
	skipUpdate := flag.Bool("no-update", false, "Skip startup update check")
	flag.Parse()

	cli.EnableANSI()
	cli.AppVersion = Version

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.LoadRaw(*envFile)
	if err != nil {
		log.Fatalf("Error loading configuration: %v", err)
	}

	loc, err := i18n.NewManager(cfg.LocaleDir, cfg.BotLanguage)
	if err != nil {
		log.Printf("Warning: %v; falling back to 'en'", err)
		loc, _ = i18n.NewManager(cfg.LocaleDir, "en")
	}

	pm, err := persona.NewManager(cfg.PersonaDir, cfg.PersonaFile)
	if err != nil {
		log.Fatalf("Error initializing personas: %v", err)
	}

	if !*headless && cli.IsInteractiveTTY() {
		if !*skipUpdate {
			updCfg := updater.Config{Version: Version}
			if info, uErr := updater.Check(ctx, updCfg); uErr == nil && info != nil {
				accepted, pErr := cli.PromptMandatoryUpdate(os.Stdin, os.Stdout, cfg, loc, pm, info.CurrentVersion, info.LatestVersion)
				if pErr != nil {
					log.Fatalf("Update prompt error: %v", pErr)
				}
				if !accepted {
					return
				}
				fmt.Printf("\n  %s\n", loc.Get("cli_update_progress"))
				if aErr := updater.Apply(ctx, updCfg, info); aErr != nil {
					fmt.Printf("  %s\n", loc.Format("cli_update_failed", aErr))
					return
				}
				fmt.Printf("  %s\n\n", loc.Get("cli_update_done"))
				if rErr := updater.Relaunch(os.Args[1:]); rErr != nil {
					log.Fatalf("Restart error: %v", rErr)
				}
				return
			}
		}

		for {
			shouldStart, updatedCfg, cliErr := cli.RunInteractive(os.Stdin, os.Stdout, *envFile, cfg, loc, pm)
			if cliErr != nil {
				log.Fatalf("CLI error: %v", cliErr)
			}
			if !shouldStart {
				return
			}
			cfg = updatedCfg
			if !runBotSession(ctx, stop, *envFile, cfg, loc, pm, true) {
				return
			}
		}
	} else if strings.TrimSpace(cfg.LLMAPIKey) == "" {
		log.Fatalf("LLM_API_KEY is required in headless mode (run without --run for interactive setup)")
	}

	runBotSession(ctx, stop, *envFile, cfg, loc, pm, false)
}

func runBotSession(parentCtx context.Context, stopApp context.CancelFunc, envPath string, cfg *config.Config, loc *i18n.Manager, pm *persona.Manager, interactiveLive bool) bool {
	ctx, cancelSession := context.WithCancel(parentCtx)
	defer cancelSession()

	st, err := store.Open(cfg.StorePath)
	if err != nil {
		log.Fatalf("Error opening store (%s): %v", cfg.StorePath, err)
	}
	defer func() {
		if err := st.Close(); err != nil {
			log.Printf("Error closing store: %v", err)
		}
	}()

	activePersona := pm.Current()
	var logBuf *cli.LogBuffer
	var activityCh chan engine.ActivityEvent
	var eventSubReady atomic.Bool
	if interactiveLive {
		logBuf = cli.NewLogBuffer(200)
		log.SetOutput(io.MultiWriter(os.Stderr, logBuf))
		defer log.SetOutput(os.Stderr)
		activityCh = make(chan engine.ActivityEvent, 64)
	}
	log.Printf("Active persona: %s [%s] | Language: %s", activePersona.Name, activePersona.BotName, loc.ActiveID())

	clk := domain.NewRealClock()
	lim := ratelimit.NewCooldownLimiter(clk, cfg.GlobalCooldown, cfg.UserCooldown)
	rm := room.NewBuffer(clk, cfg.RoomBufferSize, cfg.RoomBufferMaxAge)
	soc := social.NewEngine(clk)
	grd := safety.NewGuard(400, 480)

	llmCli := llm.NewClient(llm.ClientConfig{
		Provider:  cfg.LLMProvider,
		BaseURL:   cfg.LLMBaseURL,
		APIKey:    cfg.LLMAPIKey,
		Model:     cfg.LLMModel,
		Timeout:   cfg.LLMTimeout,
		AllowNSFW: cfg.AllowNSFW,
		Locale:    loc,
	}, clk)

	var sender domain.ChatSender
	var channelFetcher domain.ChannelInfoFetcher
	var broadcasterID string
	var botUserID string

	hasTwitchCreds := cfg.TwitchClientID != "" && cfg.TwitchBroadcasterLogin != ""

	if hasTwitchCreds {
		tm := twitch.NewTokenManager(twitch.TokenManagerConfig{
			ClientID:            cfg.TwitchClientID,
			TokenFile:           cfg.TokenFile,
			InitialAccessToken:  cfg.TwitchAccessToken,
			InitialRefreshToken: cfg.TwitchRefreshToken,
		}, clk)

		tok, err := tm.AccessToken(ctx)
		if err != nil || tok == "" {
			log.Println("No se encontró token de Twitch válido. Iniciando OAuth Device Code Flow...")
			dfr, err := tm.StartDeviceFlow(ctx, []string{"user:read:chat", "user:write:chat"})
			if err != nil {
				log.Fatalf("Error iniciando Device Flow: %v", err)
			}
			fmt.Println("=================================================================")
			fmt.Printf("¡AUTORIZACIÓN DE TWITCH REQUERIDA!\n")
			fmt.Printf("Visita: %s\n", dfr.VerificationURI)
			fmt.Printf("Código: %s\n", dfr.UserCode)
			fmt.Println("=================================================================")
			fmt.Println("Esperando confirmación en el navegador...")

			_, err = tm.PollDeviceToken(ctx, dfr.DeviceCode, time.Duration(dfr.Interval)*time.Second)
			if err != nil {
				log.Fatalf("Error autorizando token de Twitch: %v", err)
			}
			log.Println("¡Autorización exitosa! Tokens guardados en", cfg.TokenFile)
		}

		helixCli := twitch.NewHelixClient(twitch.HelixConfig{
			ClientID:     cfg.TwitchClientID,
			TokenManager: tm,
		})
		sender = helixCli
		channelFetcher = helixCli

		usersToResolve := []string{cfg.TwitchBroadcasterLogin}
		if cfg.TwitchBotLogin != "" {
			usersToResolve = append(usersToResolve, cfg.TwitchBotLogin)
		}
		resolved, err := helixCli.ResolveUsers(ctx, usersToResolve)
		if err != nil {
			log.Fatalf("Error resolviendo IDs de Twitch: %v", err)
		}

		broadcasterID = resolved[strings.ToLower(cfg.TwitchBroadcasterLogin)]
		if broadcasterID == "" {
			log.Fatalf("No se pudo resolver ID para broadcaster: %s", cfg.TwitchBroadcasterLogin)
		}
		if cfg.TwitchBotLogin != "" {
			botUserID = resolved[strings.ToLower(cfg.TwitchBotLogin)]
		}
		log.Printf("Twitch configurado: Broadcaster %s (ID: %s), Bot %s (ID: %s)",
			cfg.TwitchBroadcasterLogin, broadcasterID, cfg.TwitchBotLogin, botUserID)
	} else {
		log.Println("Sin credenciales de Twitch completas; operando con emisor estándar.")
		sender = &stdoutSender{}
	}

	orch := engine.NewOrchestrator(engine.OrchestratorConfig{
		BotUserID:   botUserID,
		BotLogin:    cfg.TwitchBotLogin,
		Clock:       clk,
		Store:       st,
		LLM:         llmCli,
		Sender:      sender,
		Persona:     pm,
		Limiter:     lim,
		Room:        rm,
		Social:      soc,
		Guard:       grd,
		Locale:      loc,
		ChannelInfo: channelFetcher,
		OnActivity: func(ev engine.ActivityEvent) {
			if activityCh == nil {
				return
			}
			select {
			case activityCh <- ev:
			default:
			}
		},
	})

	if cfg.HTTPAddr != "" {
		srv := server.NewServer(cfg.HTTPAddr, orch)
		go func() {
			log.Printf("Servidor HTTP de diagnóstico escuchando en http://%s (/healthz, /status)", cfg.HTTPAddr)
			if err := srv.Start(); err != nil && err != http.ErrServerClosed {
				log.Printf("Servidor HTTP cerrado: %v", err)
			}
		}()
		defer func() {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_ = srv.Shutdown(shutdownCtx)
		}()
	}

	if hasTwitchCreds {
		esc := twitch.NewEventSubClient(twitch.EventSubConfig{
			Clock: clk,
			OnWelcome: func(sessionID string) {
				log.Printf("Conectado a Twitch EventSub WebSocket (Session: %s). Suscribiendo...", sessionID)
				if helixClient, ok := sender.(*twitch.HelixClient); ok {
					if err := helixClient.CreateChatSubscription(ctx, sessionID, broadcasterID, botUserID); err != nil {
						log.Printf("Error suscribiendo a channel.chat.message: %v", err)
					} else {
						eventSubReady.Store(true)
						log.Printf("¡Suscripción a chat confirmada!")
					}
				}
			},
			OnChatMessage: func(msg *domain.ChatMessage) {
				if err := orch.HandleMessage(ctx, *msg); err != nil {
					log.Printf("Error procesando mensaje de chat: %v", err)
				}
			},
			OnReconnect: func(reconnectURL string) {
				log.Printf("Recibida reconexión de Twitch EventSub: %s", reconnectURL)
			},
		})

		go func() {
			if err := esc.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
				log.Printf("EventSub finalizado: %v", err)
			}
		}()
		defer func() {
			_ = esc.Close()
		}()
	}

	log.Println("Bot iniciado y en ejecución. Presiona Ctrl+C para detener.")
	backToMenu := false
	if interactiveLive {
		// Redirect standard logger exclusively into the ring buffer while the
		// alternate-screen live dashboard owns the console.
		log.SetOutput(logBuf)
		dash := cli.NewLiveDashboard(cli.LiveDashboardConfig{
			Version:       Version,
			Broadcaster:   cfg.TwitchBroadcasterLogin,
			BotLogin:      cfg.TwitchBotLogin,
			HasTwitch:     hasTwitchCreds,
			Persona:       pm,
			Orchestrator:  orch,
			Logs:          logBuf,
			Locale:        loc,
			Events:        activityCh,
			EventSubReady: &eventSubReady,
			OnPersonaSwitch: func(spec *domain.PersonaSpec) {
				if spec == nil {
					return
				}
				log.Printf("Personalidad cambiada en vivo: %s [%s]", spec.Name, spec.BotName)
				cfg.PersonaFile = filepath.Join(cfg.PersonaDir, spec.ID+".yaml")
				_ = config.SaveEnv(envPath, cfg)
			},
		})
		dash.Run(ctx,
			func() {
				backToMenu = true
				cancelSession()
			},
			func() {
				stopApp()
			},
		)
		log.SetOutput(os.Stderr)
	} else {
		<-ctx.Done()
	}
	log.Println("Señal de terminación recibida. Cerrando recursos...")
	return backToMenu
}
