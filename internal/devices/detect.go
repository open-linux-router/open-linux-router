package devices

import (
	"strings"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// Category detection: a guess, labelled as a guess, that an operator can
// override permanently.
//
// The governing rule comes from icon-style-spec.md, and it is about honesty
// rather than accuracy. A device inventory whose pictures are subtly wrong is
// worse than one that admits it only knows the category — so every rule here
// either fires on strong evidence or does not fire at all. There is no
// "probably a laptop because the vendor mostly makes laptops": a miss falls
// through to an unset category, the UI says nothing, and the operator's answer
// is asked for instead of guessed at.
//
// Two signals, in this order:
//
//  1. **Hostname.** What the client calls itself. Much the stronger signal, and
//     the one the operator can verify by looking at the device, which matters
//     when they are deciding whether to trust the guess.
//  2. **OUI.** The vendor half of the MAC. Only useful where a vendor makes
//     essentially one kind of thing; for Apple or Samsung it identifies the
//     vendor and nothing more, which is why Vendor and Category are reported
//     separately below.

// Detected is what inference produced. Category may be empty, and that is a
// result rather than a failure — it means nothing here was confident enough.
type Detected struct {
	Category Category
	Vendor   string

	// Reason names the signal that fired, so the UI can say *why* it thinks a
	// device is a printer. An unexplained guess is one an operator has no basis
	// to accept or correct.
	Reason string
}

// hostnameRule matches one token of a lower-cased hostname.
//
// Order matters: the first match wins, so more specific patterns come first.
// "appletv" must beat "apple", and "galaxytab" must beat "galaxy" or every
// Samsung tablet is filed as a phone. Note that no rule matches "switch" at
// all: on a home LAN that word is a games console about as often as it is a
// network switch, and a coin flip does not belong in an inventory.
type hostnameRule struct {
	needle   string
	category Category

	// exact requires the token to equal the needle rather than merely start
	// with it. Set for needles short or generic enough to be the opening of an
	// unrelated word: "nas" begins "nasa", "pixel" begins "pixelbook" (a
	// laptop, not a phone), "echo" begins "echolab".
	//
	// The cost is a miss on forms like "nas01". That is the right way round to
	// fail: a device that falls through to unset gets asked about once and the
	// answer is stored forever, whereas a confidently wrong category is a
	// picture the operator has to notice is wrong before they can fix it.
	exact bool
}

// Needles are matched against whole tokens (see hostnameRule.matches), so they
// never contain a separator themselves — the tokeniser also offers the whole
// name with separators stripped, which is what lets "appletv" match a host
// announcing itself as "Apple-TV-Living-Room".
var hostnameRules = []hostnameRule{
	// Media first: several contain a vendor word that later rules also match.
	{"appletv", CategoryTV, false},
	{"chromecast", CategoryTV, false},
	{"firetv", CategoryTV, false},
	{"roku", CategoryTV, false},
	{"bravia", CategoryTV, false},
	{"webos", CategoryTV, false},
	{"samsungtv", CategoryTV, false},
	{"shield", CategoryTV, true},

	{"homepod", CategorySpeaker, false},
	{"sonos", CategorySpeaker, false},
	{"echo", CategorySpeaker, true},
	{"alexa", CategorySpeaker, true},

	{"playstation", CategoryConsole, false},
	{"ps4", CategoryConsole, true},
	{"ps5", CategoryConsole, true},
	{"xbox", CategoryConsole, false},
	{"nintendo", CategoryConsole, false},
	{"steamdeck", CategoryConsole, false},

	// Personal. The tablet rules precede the phone rules they share a prefix
	// with, so "galaxytabs8" is not filed as a phone.
	{"applewatch", CategoryWatch, false},
	{"iphone", CategoryPhone, false},
	{"ipad", CategoryTablet, false},
	{"macbook", CategoryLaptop, false},
	{"imac", CategoryDesktop, true},
	{"kindle", CategoryEreader, false},
	{"kobo", CategoryEreader, true},
	{"remarkable", CategoryEreader, false},

	{"galaxytab", CategoryTablet, false},
	{"pixeltablet", CategoryTablet, false},
	{"galaxy", CategoryPhone, false},
	{"pixel", CategoryPhone, true},
	{"oneplus", CategoryPhone, false},
	{"redmi", CategoryPhone, false},
	{"xiaomi", CategoryPhone, false},

	{"thinkpad", CategoryLaptop, false},
	{"elitebook", CategoryLaptop, false},
	{"probook", CategoryLaptop, false},
	{"latitude", CategoryLaptop, false},
	{"ideapad", CategoryLaptop, false},
	{"zenbook", CategoryLaptop, false},
	{"framework", CategoryLaptop, false},
	{"laptop", CategoryLaptop, false},
	{"desktop", CategoryDesktop, false},

	// Computing and peripherals.
	{"raspberrypi", CategorySBC, false},
	{"beaglebone", CategorySBC, false},
	{"odroid", CategorySBC, false},

	{"diskstation", CategoryNAS, false},
	{"synology", CategoryNAS, false},
	{"truenas", CategoryNAS, false},
	{"freenas", CategoryNAS, false},
	{"unraid", CategoryNAS, false},
	{"qnap", CategoryNAS, false},
	{"nas", CategoryNAS, true},

	{"laserjet", CategoryPrinter, false},
	{"officejet", CategoryPrinter, false},
	{"deskjet", CategoryPrinter, false},
	{"envy", CategoryPrinter, true},
	{"brother", CategoryPrinter, false},
	{"epson", CategoryPrinter, true},
	{"canon", CategoryPrinter, true},
	{"printer", CategoryPrinter, false},

	// Home automation.
	{"doorbell", CategoryDoorbell, false},
	{"thermostat", CategoryThermostat, false},
	{"ecobee", CategoryThermostat, false},
	{"camera", CategoryCamera, false},
	{"ipcam", CategoryCamera, false},
	{"roborock", CategoryVacuum, false},
	{"roomba", CategoryVacuum, false},

	// Network infrastructure.
	{"unifi", CategoryAccessPoint, false},
	{"accesspoint", CategoryAccessPoint, false},
	{"openwrt", CategoryRouter, false},
	{"router", CategoryRouter, false},
}

// ouiEntry is one vendor prefix.
//
// Category is empty where the vendor makes many kinds of device — which is most
// of them. That is the point: an Apple OUI tells us Apple, and guessing between
// a phone, a watch, a laptop and a TV box from three octets would be inventing
// information.
type ouiEntry struct {
	vendor   string
	category Category
}

// ouiTable is a hand-seeded starter set, not the IEEE registry.
//
// It is small on purpose and will stay small. The real fix is a proper OUI
// database, which is a 30k-line generated table and a licence question, and
// which belongs behind this same function when it arrives. Until then a prefix
// that is not listed simply yields no vendor — the honest outcome — and the
// entries that do carry a category are limited to vendors whose products are
// one kind of thing.
var ouiTable = map[string]ouiEntry{
	// Single-product vendors: the category is safe.
	"b8:27:eb": {"Raspberry Pi", CategorySBC},
	"dc:a6:32": {"Raspberry Pi", CategorySBC},
	"e4:5f:01": {"Raspberry Pi", CategorySBC},
	"28:cd:c1": {"Raspberry Pi", CategorySBC},
	"00:11:32": {"Synology", CategoryNAS},
	"00:17:88": {"Philips Hue", CategoryLight},
	"00:0e:58": {"Sonos", CategorySpeaker},
	"48:a6:b8": {"Sonos", CategorySpeaker},
	"5c:aa:fd": {"Sonos", CategorySpeaker},
	"00:1b:a9": {"Brother", CategoryPrinter},
	"00:09:bf": {"Nintendo", CategoryConsole},
	"98:b6:e9": {"Nintendo", CategoryConsole},
	"e8:4e:ce": {"Nintendo", CategoryConsole},

	// Espressif: the chip inside a very large fraction of DIY and cheap
	// commercial home-automation gear. "Sensor" is the least wrong single word
	// for it and is frequently what these actually are.
	"24:0a:c4": {"Espressif", CategorySensor},
	"30:ae:a4": {"Espressif", CategorySensor},
	"84:0d:8e": {"Espressif", CategorySensor},
	"a4:cf:12": {"Espressif", CategorySensor},
	"bc:dd:c2": {"Espressif", CategorySensor},
	"cc:50:e3": {"Espressif", CategorySensor},

	// Vendor only. These make phones, tablets, laptops, watches and TV boxes;
	// the hostname rules above are what actually distinguish them.
	"a4:83:e7": {"Apple", ""},
	"ac:bc:32": {"Apple", ""},
	"dc:a9:04": {"Apple", ""},
	"f0:18:98": {"Apple", ""},
	"3c:07:54": {"Apple", ""},
	"68:a8:6d": {"Apple", ""},
	"90:b0:ed": {"Apple", ""},
	"00:12:fb": {"Samsung", ""},
	"34:23:ba": {"Samsung", ""},
	"5c:0a:5b": {"Samsung", ""},
	"78:1f:db": {"Samsung", ""},
	"00:1b:21": {"Intel", ""},
	"3c:97:0e": {"Intel", ""},
	"8c:16:45": {"Intel", ""},
	"24:a4:3c": {"Ubiquiti", ""},
	"fc:ec:da": {"Ubiquiti", ""},
	"78:8a:20": {"Ubiquiti", ""},
	"50:c7:bf": {"TP-Link", ""},
	"a4:2b:b0": {"TP-Link", ""},
}

// Detect infers what it can from a MAC and an observed hostname.
//
// It never sees stored identity, and that is deliberate: the caller applies the
// operator's answer on top (see resolve in list.go), so there is exactly one
// place where "override beats detection" is implemented and it cannot be
// bypassed by a future caller of this function.
func Detect(mac, hostname string) Detected {
	var d Detected

	if oui, ok := core.OUI(mac); ok {
		if entry, found := ouiTable[oui]; found {
			d.Vendor = entry.vendor
			if entry.category != "" {
				d.Category = entry.category
				d.Reason = "vendor " + entry.vendor
			}
		}
	}

	// Hostname second in code, first in precedence: it overwrites an
	// OUI-derived category because it is the stronger signal.
	if tokens := hostnameTokens(hostname); len(tokens) > 0 {
		for _, rule := range hostnameRules {
			if rule.matches(tokens) {
				d.Category = rule.category
				d.Reason = "hostname matches " + rule.needle
				break
			}
		}
	}

	return d
}

// matches reports whether any token satisfies the rule.
//
// Anchored to a token boundary rather than searched for anywhere in the name,
// and the difference is not academic: "nas" appears inside "jonas-laptop", and a
// substring match would confidently file someone's laptop as network storage.
func (r hostnameRule) matches(tokens []string) bool {
	for _, t := range tokens {
		if t == r.needle {
			return true
		}
		if !r.exact && strings.HasPrefix(t, r.needle) {
			return true
		}
	}
	return false
}

// hostnameTokens lower-cases the name a client announced and splits it on every
// separator clients are known to use, so that "Toms-iPhone", "toms_iphone",
// "TOMS IPHONE" and "toms.iphone.local" all yield the same tokens.
func hostnameTokens(h string) []string {
	h = strings.ToLower(strings.TrimSpace(h))
	if h == "" {
		return nil
	}
	fields := strings.FieldsFunc(h, func(r rune) bool {
		switch r {
		case '-', '_', ' ', '.', ':', ',', '\'':
			return true
		}
		return false
	})

	// The unsplit name is a token too. Some clients announce "MacBookPro" with
	// no separator at all, and a rule anchored to a token boundary would
	// otherwise never see it.
	if len(fields) > 1 {
		fields = append(fields, strings.NewReplacer(
			"-", "", "_", "", " ", "", ".", "",
		).Replace(h))
	}
	return fields
}
