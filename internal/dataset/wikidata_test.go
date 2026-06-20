package dataset

import "testing"

func TestIsQID(t *testing.T) {
	for _, s := range []string{"Q1", "Q26714", "Q100000000"} {
		if !IsQID(s) {
			t.Errorf("IsQID(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"", "Q", "Q0", "q26714", "Aquila_chrysaetos", "Q12a", "P18"} {
		if IsQID(s) {
			t.Errorf("IsQID(%q) = true, want false", s)
		}
	}
}

func TestResolveQID(t *testing.T) {
	const title = "Aquila_chrysaetos"
	enURL := "https://en.wikipedia.org/wiki/Aquila_chrysaetos"

	tests := []struct {
		name                        string
		qidTitle, qidName           string
		wantID, wantURL, wantStatus string
	}{
		{"agree", "Q26714", "Q26714", "Q26714", "", QIDAgree},
		{"name absent", "Q26714", "", "Q26714", "", QIDAgree},
		{"disagree", "Q26714", "Q999", title, enURL, QIDDisagree},
		{"unresolved", "", "Q26714", title, enURL, QIDUnresolved},
		{"both empty", "", "", title, enURL, QIDUnresolved},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			id, url, status := ResolveQID(title, tc.qidTitle, tc.qidName)
			if id != tc.wantID || url != tc.wantURL || status != tc.wantStatus {
				t.Fatalf("ResolveQID = (%q,%q,%q), want (%q,%q,%q)",
					id, url, status, tc.wantID, tc.wantURL, tc.wantStatus)
			}
		})
	}
}

func TestWikipediaEnURL(t *testing.T) {
	if got := WikipediaEnURL("Aquila_chrysaetos"); got != "https://en.wikipedia.org/wiki/Aquila_chrysaetos" {
		t.Fatalf("got %q", got)
	}
	// A space (should not occur in stored ids, but must encode safely) becomes %20.
	if got := WikipediaEnURL("Golden eagle"); got != "https://en.wikipedia.org/wiki/Golden%20eagle" {
		t.Fatalf("got %q", got)
	}
}

func TestAspectRatio(t *testing.T) {
	if got := AspectRatio(1600, 900); got != 1.78 {
		t.Fatalf("AspectRatio(1600,900) = %v, want 1.78", got)
	}
	if got := AspectRatio(800, 1200); got != 0.67 {
		t.Fatalf("AspectRatio(800,1200) = %v, want 0.67", got)
	}
	if got := AspectRatio(100, 0); got != 0 {
		t.Fatalf("AspectRatio(100,0) = %v, want 0", got)
	}
}

func TestClassifyOrientation(t *testing.T) {
	cases := []struct {
		w, h int
		want string
	}{
		{1600, 900, "landscape"},
		{800, 1200, "portrait"},
		{1000, 1000, "square"},
		{0, 0, ""},
	}
	for _, c := range cases {
		if got := ClassifyOrientation(c.w, c.h); got != c.want {
			t.Errorf("ClassifyOrientation(%d,%d) = %q, want %q", c.w, c.h, got, c.want)
		}
	}
}

func TestAcceptLicense(t *testing.T) {
	good := []string{
		"https://creativecommons.org/licenses/by-sa/4.0/",
		"https://creativecommons.org/licenses/by/2.0",
		"https://creativecommons.org/publicdomain/zero/1.0/",
		"http://creativecommons.org/licenses/by-sa/3.0/",
	}
	for _, l := range good {
		if !AcceptLicense(l) {
			t.Errorf("AcceptLicense(%q) = false, want true", l)
		}
	}
	bad := []string{
		"", "https://example.com/all-rights-reserved", "fair use",
		"https://en.wikipedia.org/wiki/Public_domain",
		// host is not creativecommons.org; the CC substring is only a spoof in the query.
		"https://evil.example/?ref=creativecommons.org/licenses/by/4.0/",
		// right host, wrong path.
		"https://creativecommons.org/about/",
	}
	for _, l := range bad {
		if AcceptLicense(l) {
			t.Errorf("AcceptLicense(%q) = true, want false", l)
		}
	}
}

func TestSanitizeCreator(t *testing.T) {
	in := `<a href="//commons.wikimedia.org/wiki/User:Jane" title="User:Jane">Jane&nbsp;Doe</a>`
	if got := SanitizeCreator(in); got != "Jane Doe" {
		t.Fatalf("SanitizeCreator = %q, want %q", got, "Jane Doe")
	}
	if got := SanitizeCreator("  Plain   Name "); got != "Plain Name" {
		t.Fatalf("SanitizeCreator = %q", got)
	}
}

func TestParsePageProps(t *testing.T) {
	data := []byte(`{"query":{
      "normalized":[{"from":"Aquila_chrysaetos","to":"Aquila chrysaetos"}],
      "redirects":[{"from":"Old name","to":"New name"}],
      "pages":{
        "12345":{"title":"Aquila chrysaetos","pageprops":{"wikibase_item":"Q26714"}},
        "67890":{"title":"New name","pageprops":{"wikibase_item":"Q999"}},
        "-1":{"title":"Missing thing"}
      }}}`)
	res, err := ParsePageProps(data)
	if err != nil {
		t.Fatal(err)
	}
	if got := res.QIDForTitle("Aquila_chrysaetos"); got != "Q26714" {
		t.Errorf("normalized lookup = %q, want Q26714", got)
	}
	if got := res.QIDForTitle("Old name"); got != "Q999" {
		t.Errorf("redirect lookup = %q, want Q999", got)
	}
	if got := res.QIDForTitle("Missing thing"); got != "" {
		t.Errorf("missing pageprops lookup = %q, want empty", got)
	}
}

func TestParseSPARQL(t *testing.T) {
	data := []byte(`{"results":{"bindings":[
      {"taxon":{"value":"http://www.wikidata.org/entity/Q26714"},"name":{"value":"Aquila chrysaetos"}},
      {"taxon":{"value":"http://www.wikidata.org/entity/Q111"},"name":{"value":"Homonymus dup"}},
      {"taxon":{"value":"http://www.wikidata.org/entity/Q222"},"name":{"value":"Homonymus dup"}}
    ]}}`)
	got, err := ParseSPARQL(data)
	if err != nil {
		t.Fatal(err)
	}
	if got["Aquila chrysaetos"] != "Q26714" {
		t.Errorf("single match = %q, want Q26714", got["Aquila chrysaetos"])
	}
	if _, ok := got["Homonymus dup"]; ok {
		t.Errorf("ambiguous name should be omitted, got %q", got["Homonymus dup"])
	}
}

func TestParseP18(t *testing.T) {
	data := []byte(`{"entities":{
      "Q26714":{"claims":{"P18":[{"mainsnak":{"datavalue":{"value":"Golden Eagle in flight.jpg"}}}]}},
      "Q999":{"claims":{}}
    }}`)
	got, err := ParseP18(data)
	if err != nil {
		t.Fatal(err)
	}
	if got["Q26714"] != "Golden Eagle in flight.jpg" {
		t.Errorf("P18 = %q", got["Q26714"])
	}
	if _, ok := got["Q999"]; ok {
		t.Errorf("entity without P18 should be omitted")
	}
}

func TestParsersRejectMalformedJSON(t *testing.T) {
	bad := []byte("{not valid json")
	if _, err := ParsePageProps(bad); err == nil {
		t.Error("ParsePageProps: want error on malformed JSON, got nil")
	}
	if _, err := ParseSPARQL(bad); err == nil {
		t.Error("ParseSPARQL: want error on malformed JSON, got nil")
	}
	if _, err := ParseP18(bad); err == nil {
		t.Error("ParseP18: want error on malformed JSON, got nil")
	}
	if _, err := ParseImageInfo(bad); err == nil {
		t.Error("ParseImageInfo: want error on malformed JSON, got nil")
	}
}

func TestQIDForTitleChain(t *testing.T) {
	// A queried title that is first normalized and then redirected must follow
	// both hops to reach the page that carries the QID.
	data := []byte(`{"query":{
      "normalized":[{"from":"x","to":"Y"}],
      "redirects":[{"from":"Y","to":"Z"}],
      "pages":{"1":{"title":"Z","pageprops":{"wikibase_item":"Q7"}}}}}`)
	res, err := ParsePageProps(data)
	if err != nil {
		t.Fatal(err)
	}
	if got := res.QIDForTitle("x"); got != "Q7" {
		t.Errorf("normalize+redirect chain = %q, want Q7", got)
	}
	// "Y" is itself the redirect source (Y->Z), so it resolves to Z's QID too.
	if got := res.QIDForTitle("Y"); got != "Q7" {
		t.Errorf("redirect-source lookup = %q, want Q7", got)
	}
	// A title absent from every map resolves to empty.
	if got := res.QIDForTitle("Unknown"); got != "" {
		t.Errorf("unknown title = %q, want empty", got)
	}
}

func TestParseSPARQLSkips(t *testing.T) {
	data := []byte(`{"results":{"bindings":[
      {"taxon":{"value":"http://www.wikidata.org/entity/Q1"},"name":{"value":"Good name"}},
      {"name":{"value":"No taxon binding"}},
      {"taxon":{"value":"http://www.wikidata.org/entity/not-a-qid"},"name":{"value":"Bad qid"}},
      {"taxon":{"value":"bareValueNoSlash"},"name":{"value":"No slash"}}
    ]}}`)
	got, err := ParseSPARQL(data)
	if err != nil {
		t.Fatal(err)
	}
	if got["Good name"] != "Q1" {
		t.Errorf("good binding = %q, want Q1", got["Good name"])
	}
	for _, skipped := range []string{"No taxon binding", "Bad qid", "No slash"} {
		if _, ok := got[skipped]; ok {
			t.Errorf("expected %q to be skipped", skipped)
		}
	}
	if len(got) != 1 {
		t.Errorf("len = %d, want 1 (only the valid binding)", len(got))
	}
}

func TestClassifyOrientationBoundaries(t *testing.T) {
	cases := []struct {
		w, h int
		want string
	}{
		{115, 100, "landscape"}, // exactly the 1.15 threshold
		{114, 100, "square"},    // just below
		{87, 100, "portrait"},   // exactly the 0.87 threshold
		{88, 100, "square"},     // just above
		{400, 300, "landscape"}, // 4:3 reads as landscape
	}
	for _, c := range cases {
		if got := ClassifyOrientation(c.w, c.h); got != c.want {
			t.Errorf("ClassifyOrientation(%d,%d) = %q, want %q", c.w, c.h, got, c.want)
		}
	}
}

func TestAspectRatioNegativeAndRounding(t *testing.T) {
	if got := AspectRatio(-5, 10); got != 0 {
		t.Errorf("AspectRatio(-5,10) = %v, want 0", got)
	}
	if got := AspectRatio(3, 7); got != 0.43 {
		t.Errorf("AspectRatio(3,7) = %v, want 0.43", got)
	}
}

func TestAcceptLicenseNormalization(t *testing.T) {
	if !AcceptLicense("  HTTPS://CreativeCommons.org/Licenses/by/4.0/  ") {
		t.Error("AcceptLicense should trim and lowercase before matching")
	}
}

func TestParseP18EmptyAndMulti(t *testing.T) {
	data := []byte(`{"entities":{
      "Q1":{"claims":{"P18":[
        {"mainsnak":{"datavalue":{"value":"First.jpg"}}},
        {"mainsnak":{"datavalue":{"value":"Second.jpg"}}}]}},
      "Q2":{"claims":{"P18":[{"mainsnak":{"datavalue":{"value":""}}}]}}
    }}`)
	got, err := ParseP18(data)
	if err != nil {
		t.Fatal(err)
	}
	if got["Q1"] != "First.jpg" {
		t.Errorf("multi-claim P18 = %q, want First.jpg (first value)", got["Q1"])
	}
	if _, ok := got["Q2"]; ok {
		t.Error("empty P18 filename should be omitted")
	}
}

func TestParseImageInfoNormalizeRenameAndEmpty(t *testing.T) {
	data := []byte(`{"query":{
      "normalized":[{"from":"file:x.jpg","to":"File:X.jpg"}],
      "pages":{
        "1":{"title":"File:X.jpg","imageinfo":[{"width":10,"height":10,"mime":"image/jpeg",
          "extmetadata":{"LicenseUrl":{"value":"https://creativecommons.org/licenses/by/4.0/"}}}]},
        "2":{"title":"File:Empty.jpg"}
      }}}`)
	res, err := ParseImageInfo(data)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := res.FileFor("file:x.jpg"); !ok {
		t.Error("FileFor should follow the normalization rename to find the file")
	}
	if _, ok := res.FileFor("File:Empty.jpg"); ok {
		t.Error("a page with no imageinfo should not appear in Files")
	}
}

func TestParseImageInfoFollowsRedirect(t *testing.T) {
	// A P18 value can name a since-renamed file; with redirects=1 the API keys the
	// page under the new title and reports the old->new redirect.
	data := []byte(`{"query":{
      "redirects":[{"from":"File:Old name.jpg","to":"File:New name.jpg"}],
      "pages":{"1":{"title":"File:New name.jpg","imageinfo":[{"width":20,"height":10,"mime":"image/jpeg",
        "extmetadata":{"LicenseUrl":{"value":"https://creativecommons.org/licenses/by-sa/4.0/"}}}]}}}}`)
	res, err := ParseImageInfo(data)
	if err != nil {
		t.Fatal(err)
	}
	f, ok := res.FileFor("File:Old name.jpg")
	if !ok {
		t.Fatal("FileFor should follow a Commons redirect to the renamed file")
	}
	if f.Filename != "New name.jpg" {
		t.Errorf("resolved filename = %q, want New name.jpg", f.Filename)
	}
}

func TestParseImageInfoAndBuild(t *testing.T) {
	data := []byte(`{"query":{
      "normalized":[{"from":"File:Golden Eagle in flight.jpg","to":"File:Golden Eagle in flight.jpg"}],
      "pages":{"42":{"title":"File:Golden Eagle in flight.jpg","imageinfo":[{
        "width":1600,"height":900,"mime":"image/jpeg",
        "extmetadata":{
          "LicenseUrl":{"value":"https://creativecommons.org/licenses/by-sa/4.0/"},
          "Artist":{"value":"<a href=\"x\">Jane Doe</a>"}
        }}]}}}}`)
	res, err := ParseImageInfo(data)
	if err != nil {
		t.Fatal(err)
	}
	f, ok := res.FileFor("File:Golden Eagle in flight.jpg")
	if !ok {
		t.Fatal("FileFor: not found")
	}
	if f.Filename != "Golden Eagle in flight.jpg" || f.Width != 1600 || f.MIME != "image/jpeg" {
		t.Fatalf("unexpected file: %+v", f)
	}
	item, ok := BuildWikimediaMedia(f)
	if !ok {
		t.Fatal("BuildWikimediaMedia rejected a CC-licensed file")
	}
	if item.Source != "wikimedia" || item.ID != "Golden Eagle in flight.jpg" {
		t.Fatalf("item source/id = %+v", item)
	}
	if item.Orientation != "landscape" || item.AspectRatio != 1.78 {
		t.Fatalf("item layout = %+v", item)
	}
	if item.Creator != "Jane Doe" || item.RightsHolder != "Jane Doe" {
		t.Fatalf("item creator = %+v", item)
	}

	// A file with an unclean license yields no media item.
	bad := CommonsFile{Filename: "x.jpg", Width: 10, Height: 10, MIME: "image/jpeg", LicenseURL: ""}
	if _, ok := BuildWikimediaMedia(bad); ok {
		t.Fatal("BuildWikimediaMedia accepted a file with no license")
	}
}
