package cli

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/jpillora/go-ogle-analytics"
	"github.com/kopia/kopia/internal/ospath"
	"github.com/kopia/kopia/repo"
	"github.com/rs/zerolog/log"
)

const googleAnalyticsID = "UA-256960-23"

var (
	clientIDFile     = app.Flag("client-id-file", "Path to client ID file, which enables anonymous usage reporting if present").Default(filepath.Join(ospath.ConfigDir(), "client_id.txt")).String()
	analyticsConsent = app.Flag("analytics-consent", "Consent to send analytics").Default("ask").Enum("yes", "no", "ask")

	globalGAClient *ga.Client
	gaClientOnce   sync.Once
)

func analyticsUserAgent() string {
	return "kopia-" + repo.BuildVersion + "-" + runtime.GOARCH + "-" + runtime.GOOS
}

func enableAnalytics(clientID string) {
	f, err := os.Create(*clientIDFile)
	if err != nil {
		log.Warn().Err(err).Msg("unable to write client ID")
		return
	}

	defer f.Close() //nolint:errcheck
	fmt.Fprintf(f, "%v", clientID)
}

func disableAnalytics() {
	// the user does NOT want to provide analytics consent, delete client ID file, if any.
	os.Remove(*clientIDFile) //nolint:errcheck
}

func displayAnalyticsConsentPrompt(clientID string) bool {
	var consent string
	fmt.Println()
	fmt.Println("Help improve Kopia by sharing anonymous usage statistics with developers:")
	fmt.Println()
	fmt.Println("The information includes:")
	fmt.Println("  Current build version:       ", analyticsUserAgent())
	fmt.Println("  Random anonymous identifier: ", clientID)
	fmt.Println("  Type and version of storage: ", "(e.g. 'gcs-v1', 's3-v1')")
	fmt.Println("  Name of the feature used:    ", "(e.g. 'snapshot create', 'policy set')")
	fmt.Println()
	fmt.Println("No personal information (user names, host names, filenames, etc.) is ever sent:")
	fmt.Println()
	fmt.Print("Do you agree? (y/n) ")
	fmt.Scanf("%s", &consent)
	if !strings.HasPrefix(strings.ToLower(consent), "y") {
		return false
	}
	fmt.Println("Thank you for sharing!")
	return true
}

func promptForAnalyticsConsent() {
	// generate 64-bit ID as 16 hex digits.
	x := make([]byte, 8)
	rand.Read(x) //nolint:errcheck
	clientID := fmt.Sprintf("%x", x)

	switch *analyticsConsent {
	case "no":
		disableAnalytics()

	case "yes":
		enableAnalytics(clientID)

	case "ask":
		if displayAnalyticsConsentPrompt(clientID) {
			enableAnalytics(clientID)
		} else {
			disableAnalytics()
		}
	}
}

// initGAClient sets up Google Analytics reporting for this session.
func initGAClient() *ga.Client {
	gaClientOnce.Do(func() {
		f, err := os.Open(*clientIDFile)
		if os.IsNotExist(err) {
			log.Printf("not reporting usage - %v does not exist", *clientIDFile)
			return
		}
		if err != nil {
			log.Warn().Err(err).Msg("unable to open client ID")
		}
		defer f.Close() //nolint:errcheck

		var clientID string
		if _, serr := fmt.Fscanf(f, "%s", &clientID); serr != nil {
			log.Printf("not reporting usage - invalid client ID file: %v", *clientIDFile)
			return
		}

		client, err := ga.NewClient(googleAnalyticsID)
		if err != nil {
			// this should not happen because the UA is valid.
			panic("unable to initialize GA client: " + err.Error())
		}

		userAgent := analyticsUserAgent()
		log.Printf("analytics userAgent: %q, clientID: %q", userAgent, clientID)

		globalGAClient = client.
			ClientID(clientID).
			UserAgentOverride(userAgent).
			AnonymizeIP(true)
	})
	return globalGAClient
}

// reportStartupTime reports startup time.
func reportStartupTime(storageType string, formatVersion int, startupDuration time.Duration) {
	if gaClient := initGAClient(); gaClient != nil && *analyticsConsent != "no" {
		log.Printf("reporting startup duration %v", startupDuration)
		gaClient.Send( //nolint:errcheck
			ga.NewEvent("initialize", fmt.Sprintf("%v-v%v", storageType, formatVersion)).
				Value(startupDuration.Nanoseconds() / 1e6))
	}
}

// reportSubcommandFinished reports a single subcommand usage.
func reportSubcommandFinished(commandType string, success bool, storageType string, formatVersion int, duration time.Duration) {
	if gaClient := initGAClient(); gaClient != nil && *analyticsConsent != "no" {
		log.Printf("reporting command %v finished (success=%v, duration=%v)", commandType, success, duration)
		if success {
			gaClient.Send(ga.NewEvent("command-success", commandType).Label(fmt.Sprintf("%v-v%v", storageType, formatVersion)).Value(duration.Nanoseconds() / 1e6)) //nolint:errcheck
		} else {
			gaClient.Send(ga.NewEvent("command-failed", commandType).Label(fmt.Sprintf("%v-v%v", storageType, formatVersion)).Value(duration.Nanoseconds() / 1e6)) //nolint:errcheck
		}
	}
}