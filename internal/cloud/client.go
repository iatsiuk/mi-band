// Package cloud logs into a Xiaomi account and reads the Mi Fitness health cloud: the device list,
// which carries the BLE auth key (encrypt_key) of bound wearables, fitness records, workouts and
// the raw band files kept in FDS. Re-implemented from the Mi Fitness app (CloudInterceptor,
// n83/i83/qye, XMPassport, FitnessDataRequest, FitnessFDSUploader). Nothing is stored on disk.
package cloud

import (
	"crypto/md5" //nolint:gosec // user agent hashes are md5 by protocol
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const (
	// service id of the Mi Fitness health cloud (SIDs.MIOT_HEALTH)
	healthSID = "miothealth"

	defaultAccountURL = "https://account.xiaomi.com"
	healthHost        = "hlth.io.mi.com"
	defaultLocale     = "en_US"

	// identity of the app build the protocol was taken from (AndroidManifest + account sdk BuildConfig)
	appPackage     = "com.mi.health"
	appVersionName = "9.9.772"
	appVersionCode = "909772"
	sdkVersion     = "5.3.0.release.66"
	phoneRelease   = "14"
	phoneBrand     = "Xiaomi"
	phoneModel     = "23127PN0CG"
)

// Options configures Login. The zero value targets the China health cloud.
type Options struct {
	// Region is the Mi Fitness server region chosen in the app (for example "de", "sg", "ru",
	// "us", "i2"). Empty or "cn" selects hlth.io.mi.com; other values select <region>.hlth.io.mi.com.
	Region string
	// Locale in Java form, for example "en_US". Defaults to en_US.
	Locale string
	// DeviceID is the passport deviceId cookie. Reusing the same value across logins lets the
	// account remember a verified device. Defaults to a random "android_<uuid>".
	DeviceID string
	// HTTPClient is used for all requests; redirects and cookie jars are disabled on a copy.
	HTTPClient *http.Client
	// AccountURL and HealthURL override the passport and health cloud base URLs (tests, proxies).
	AccountURL string
	HealthURL  string
	// Logf, if set, receives one line per health cloud call (method, endpoint and response size) and
	// per identity check call (method, path and result code). Queries and bodies are never logged:
	// they hold device keys, signed urls, login context and health data.
	Logf func(format string, args ...any)
}

func (o *Options) withDefaults() (Options, error) {
	out := *o
	out.Region = strings.ToLower(strings.TrimSpace(out.Region))
	if out.Locale == "" {
		out.Locale = defaultLocale
	}
	if out.DeviceID == "" {
		id, err := pseudoDeviceID()
		if err != nil {
			return out, err
		}
		out.DeviceID = id
	}
	if out.AccountURL == "" {
		out.AccountURL = defaultAccountURL
	}
	out.AccountURL = strings.TrimRight(out.AccountURL, "/")
	if out.HealthURL == "" {
		out.HealthURL = healthBaseURL(out.Region)
	}
	out.HealthURL = strings.TrimRight(out.HealthURL, "/")
	c := http.Client{Timeout: 30 * time.Second}
	if out.HTTPClient != nil {
		c = *out.HTTPClient
	}
	c.Jar = nil
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	out.HTTPClient = &c
	return out, nil
}

// healthBaseURL mirrors RegionUrlSwitcherImpl: non-cn regions prefix the host with "<region>.".
func healthBaseURL(region string) string {
	if region == "" || region == "cn" {
		return "https://" + healthHost
	}
	return "https://" + region + "." + healthHost
}

// pseudoDeviceID mirrors HashedDeviceIdUtil.createPseudoDeviceId.
func pseudoDeviceID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	h := hex.EncodeToString(b[:])
	return fmt.Sprintf("android_%s-%s-%s-%s-%s", h[:8], h[8:12], h[12:16], h[16:20], h[20:]), nil
}

// passportUserAgent mirrors XMPassportUserAgent: <http.agent> APP/ APPV/ MK/ SDKV/.
func passportUserAgent() string {
	pkg := appPackage[strings.Index(appPackage, ".")+1:]
	return fmt.Sprintf("Dalvik/2.1.0 (Linux; U; Android %s; %s) APP/%s APPV/%s MK/%s SDKV/%s",
		phoneRelease, phoneModel, pkg, appVersionCode,
		base64.StdEncoding.EncodeToString([]byte(phoneModel)), sdkVersion)
}

// webViewUserAgent mirrors the PassportJsbWebView that hosts the identity check: the system WebView
// UA, the XMPassportUserAgent fields with XiaoMi/HybridView/, then WebViewType/PassportJSBWebView
// (UserAgentBuilder.build, PassportJsbWebView.java:302). The WebView part is a stock Android 14 one.
func webViewUserAgent() string {
	pkg := appPackage[strings.Index(appPackage, ".")+1:]
	return fmt.Sprintf("Mozilla/5.0 (Linux; Android %s; %s Build/UKQ1.231003.002; wv) AppleWebKit/537.36 (KHTML, like Gecko) "+
		"Version/4.0 Chrome/124.0.6367.179 Mobile Safari/537.36 APP/%s APPV/%s XiaoMi/HybridView/ MK/%s SDKV/%s WebViewType/PassportJSBWebView",
		phoneRelease, phoneModel, pkg, appVersionCode, base64.StdEncoding.EncodeToString([]byte(phoneModel)), sdkVersion)
}

// healthUserAgent mirrors UAInterceptor: Android-<release>-<version>-<brand>-<model>-md5(deviceId)-md5(userId).
func healthUserAgent(deviceID, userID string) string {
	return fmt.Sprintf("Android-%s-%s-%s-%s-%s-%s",
		phoneRelease, appVersionName, phoneBrand, phoneModel, md5Hex(deviceID), md5Hex(userID))
}

// healthLocale mirrors LocaleUtil.getCurrentLocale(true, false): language_country, lower case.
func healthLocale(locale string) string {
	return strings.ToLower(strings.ReplaceAll(locale, "-", "_"))
}

func md5Hex(s string) string {
	if s == "" {
		return ""
	}
	sum := md5.Sum([]byte(s)) //nolint:gosec // protocol-defined user agent hash
	return hex.EncodeToString(sum[:])
}
