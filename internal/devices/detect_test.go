package devices

import "testing"

func TestDetectFromHostname(t *testing.T) {
	tests := []struct {
		hostname string
		want     Category
	}{
		{"Toms-iPhone", CategoryPhone},
		{"toms_iphone", CategoryPhone},
		{"TOMS IPHONE", CategoryPhone},
		{"iPhone", CategoryPhone},
		// No separator at all, which is how several clients announce themselves.
		{"MacBookPro", CategoryLaptop},
		{"macbook-air", CategoryLaptop},
		{"iPad-Pro", CategoryTablet},
		{"kindle-paperwhite", CategoryEreader},
		{"DiskStation", CategoryNAS},
		{"office-nas", CategoryNAS},
		{"raspberrypi", CategorySBC},
		{"HP-LaserJet-M140we", CategoryPrinter},
		{"living-room-appletv", CategoryTV},
		{"Apple-TV-Living-Room", CategoryTV},
		{"Chromecast-Bedroom", CategoryTV},
		{"Sonos-Kitchen", CategorySpeaker},
		{"PlayStation-5", CategoryConsole},
		{"front-doorbell", CategoryDoorbell},
		{"unifi-ap-loft", CategoryAccessPoint},
	}
	for _, tc := range tests {
		got := Detect("aa:bb:cc:dd:ee:ff", tc.hostname)
		if got.Category != tc.want {
			t.Errorf("Detect(hostname=%q).Category = %q, want %q",
				tc.hostname, got.Category, tc.want)
		}
		if got.Reason == "" {
			t.Errorf("Detect(hostname=%q) gave no reason; an unexplained guess is one "+
				"an operator has no basis to accept", tc.hostname)
		}
	}
}

// The bug a naive strings.Contains would have: "nas" sits inside "jonas", and a
// substring match would confidently file someone's laptop as network storage.
func TestDetectDoesNotMatchInsideAWord(t *testing.T) {
	tests := []struct {
		hostname string
		notWant  Category
	}{
		{"jonas-laptop", CategoryNAS},
		{"jonas", CategoryNAS},
		{"thomas", CategoryNAS},
		{"nasa-workstation", CategoryNAS},
	}
	for _, tc := range tests {
		got := Detect("aa:bb:cc:dd:ee:ff", tc.hostname)
		if got.Category == tc.notWant {
			t.Errorf("Detect(hostname=%q).Category = %q; %q appears only inside a word",
				tc.hostname, got.Category, "nas")
		}
	}

	// The positive control: "jonas-laptop" should still be read as a laptop.
	if got := Detect("aa:bb:cc:dd:ee:ff", "jonas-laptop"); got.Category != CategoryLaptop {
		t.Errorf("Detect(\"jonas-laptop\").Category = %q, want %q", got.Category, CategoryLaptop)
	}
}

func TestDetectPrefersTheMoreSpecificRule(t *testing.T) {
	// "galaxytab" must beat "galaxy", or every tablet is filed as a phone.
	if got := Detect("aa:bb:cc:dd:ee:ff", "Galaxy-Tab-S8"); got.Category != CategoryTablet {
		t.Errorf("Galaxy-Tab-S8 = %q, want %q", got.Category, CategoryTablet)
	}
	if got := Detect("aa:bb:cc:dd:ee:ff", "Galaxy-S21"); got.Category != CategoryPhone {
		t.Errorf("Galaxy-S21 = %q, want %q", got.Category, CategoryPhone)
	}
}

func TestDetectFromOUI(t *testing.T) {
	// A globally-administered Raspberry Pi prefix: vendor makes one kind of
	// thing, so the category is safe.
	got := Detect("b8:27:eb:11:22:33", "")
	if got.Vendor != "Raspberry Pi" {
		t.Errorf("Vendor = %q, want %q", got.Vendor, "Raspberry Pi")
	}
	if got.Category != CategorySBC {
		t.Errorf("Category = %q, want %q", got.Category, CategorySBC)
	}
}

// Apple makes phones, tablets, laptops, watches and TV boxes. Three octets
// cannot choose between them, and inventing an answer is exactly what
// icon-style-spec.md forbids.
func TestDetectFromOUIGivesVendorWithoutGuessingCategory(t *testing.T) {
	got := Detect("a4:83:e7:11:22:33", "")
	if got.Vendor != "Apple" {
		t.Errorf("Vendor = %q, want %q", got.Vendor, "Apple")
	}
	if got.Category != "" {
		t.Errorf("Category = %q, want it unset: the vendor makes many kinds of device",
			got.Category)
	}
}

func TestHostnameBeatsOUI(t *testing.T) {
	// Espressif's prefix implies a sensor, but the device says it is a doorbell.
	got := Detect("24:0a:c4:11:22:33", "front-doorbell")
	if got.Category != CategoryDoorbell {
		t.Errorf("Category = %q, want %q: the hostname is the stronger signal",
			got.Category, CategoryDoorbell)
	}
	// The vendor survives, because it is a separate fact.
	if got.Vendor != "Espressif" {
		t.Errorf("Vendor = %q, want it preserved", got.Vendor)
	}
}

func TestDetectGivesNothingWhenItKnowsNothing(t *testing.T) {
	got := Detect("aa:bb:cc:dd:ee:ff", "some-random-box")
	if got.Category != "" {
		t.Errorf("Category = %q, want it unset rather than a guess", got.Category)
	}
	if got.Vendor != "" {
		t.Errorf("Vendor = %q, want it empty for an unlisted, locally-administered prefix", got.Vendor)
	}
}

func TestDetectToleratesAnUnparseableMAC(t *testing.T) {
	// Should not panic, and should still read the hostname.
	got := Detect("nonsense", "Toms-iPhone")
	if got.Category != CategoryPhone {
		t.Errorf("Category = %q, want %q", got.Category, CategoryPhone)
	}
}
