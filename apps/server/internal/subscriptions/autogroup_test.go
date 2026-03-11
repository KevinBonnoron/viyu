package subscriptions

import (
	"testing"

	"ganigeorgiev.com/pocketbase/dbx"
)

func TestChannelNameKeyIgnoresCaseAccentsAndPunctuation(t *testing.T) {
	t.Parallel()
	for name, want := range map[string]string{
		"ZeratoR":   "zerator",
		"zerator":   "zerator",
		"Angèle":    "angele",
		"Mister V!": "misterv",
		"Squeezie_": "squeezie",
		"ＫＡＭＥＴＯ０":   "kameto0", // compatibility forms
		"   ":       "",
		"***":       "",
		"日本語チャンネル":  "日本語チャンネル",
	} {
		if got := channelNameKey(name); got != want {
			t.Errorf("channelNameKey(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestChannelNameSegmentKeysSplitBundledNames(t *testing.T) {
	t.Parallel()
	for name, want := range map[string][]string{
		"Yves Rougy | Yorzian": {"yvesrougy", "yorzian"},
		"Yorzian (Twitch)":     {"yorzian", "twitch"},
		"A / B / Gaming":       {"gaming"}, // one or two letters never match
		"Jean-Michel":          nil,        // a hyphen without spaces is part of a name
		"Yorzian":              nil,
		"Yorzian | Yorzian":    {"yorzian"},
	} {
		got := channelNameSegmentKeys(name)
		if len(got) != len(want) {
			t.Errorf("channelNameSegmentKeys(%q) = %v, want %v", name, got, want)
			continue
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("channelNameSegmentKeys(%q) = %v, want %v", name, got, want)
			}
		}
	}
}

func TestAutoGroupMatchesABundledNameAgainstAWholeName(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	profile := f.profile(t, f.user(t, "autogroup-bundled@example.test"), "Bundled")
	first := f.subscribe(t, profile, alpha, "yves", "Yves Rougy | Yorzian")
	second := f.subscribe(t, profile, beta, "yorzian", "Yorzian")
	// Two unrelated creators that merely share a part of their name stay apart.
	f.subscribe(t, profile, alpha, "foo", "Foo | Gaming")
	f.subscribe(t, profile, beta, "bar", "Bar | Gaming")

	created, err := f.service.AutoGroup(profile.Id)
	if err != nil || created != 1 {
		t.Fatalf("created=%d err=%v, want one group", created, err)
	}
	if creator := f.creator(t, first.Id); creator == "" || f.creator(t, second.Id) != creator {
		t.Fatal("\"Yves Rougy | Yorzian\" and \"Yorzian\" were not linked")
	}
	if n := f.count(t, subscriptionsCollection, dbx.NewExp("creator != ''")); n != 2 {
		t.Fatalf("only the bundled pair may be grouped, got %d members", n)
	}
}

func TestAutoGroupLeavesABundledNameAloneWhenAWholeNameAlreadyMatches(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	profile := f.profile(t, f.user(t, "autogroup-ambiguous@example.test"), "Ambiguous")
	bundled := f.subscribe(t, profile, alpha, "one", "Someone | Yorzian")
	f.subscribe(t, profile, alpha, "two", "Yorzian")
	f.subscribe(t, profile, beta, "yorzian", "Yorzian")
	// The exact "Yorzian" pair is the one to link; the bundled name is not guessed on top of it.
	created, err := f.service.AutoGroup(profile.Id)
	if err != nil || created != 1 {
		t.Fatalf("created=%d err=%v, want the exact pair only", created, err)
	}
	if f.creator(t, bundled.Id) != "" {
		t.Fatal("the bundled name was grouped next to an exact match")
	}
}

func TestAutoGroupLinksOnlyObviousCrossPlatformCreators(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	profile := f.profile(t, f.user(t, "autogroup@example.test"), "Auto")
	first := f.subscribe(t, profile, alpha, "zerator-a", "ZeratoR")
	second := f.subscribe(t, profile, beta, "zerator-b", "zerator")
	unrelated := f.subscribe(t, profile, alpha, "other", "Someone else")
	// Two channels of one platform and one of another share a name: nobody can say which is which.
	f.subscribe(t, profile, alpha, "ambiguous-1", "Ambiguous")
	f.subscribe(t, profile, alpha, "ambiguous-2", "Ambiguous")
	f.subscribe(t, profile, beta, "ambiguous-3", "Ambiguous")

	created, err := f.service.AutoGroup(profile.Id)
	if err != nil || created != 1 {
		t.Fatalf("created=%d err=%v, want one group", created, err)
	}
	creator := f.creator(t, first.Id)
	if creator == "" || f.creator(t, second.Id) != creator {
		t.Fatalf("the two channels of one creator were not linked: %q %q", creator, f.creator(t, second.Id))
	}
	if f.creator(t, unrelated.Id) != "" {
		t.Fatal("an unrelated channel was grouped")
	}
	if n := f.count(t, subscriptionsCollection, dbx.NewExp("creator != ''")); n != 2 {
		t.Fatalf("only the unambiguous pair may be grouped, got %d members", n)
	}
	if again, err := f.service.AutoGroup(profile.Id); err != nil || again != 0 {
		t.Fatalf("grouping must be idempotent: %d %v", again, err)
	}
}

func TestAutoGroupNeverTouchesASeparatedSubscriptionOrOneThatHasACreator(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	profile := f.profile(t, f.user(t, "autogroup-kept@example.test"), "Kept")
	first := f.subscribe(t, profile, alpha, "kept-a", "Kept Creator")
	second := f.subscribe(t, profile, beta, "kept-b", "Kept Creator")
	f.setUngrouped(t, first)
	if created, err := f.service.AutoGroup(profile.Id); err != nil || created != 0 {
		t.Fatalf("a separated subscription was merged again: %d %v", created, err)
	}
	if f.creator(t, first.Id) != "" || f.creator(t, second.Id) != "" {
		t.Fatal("a creator was written next to a separated subscription")
	}
}

func TestAutoGroupStaysInsideOneProfile(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	owner := f.user(t, "autogroup-profiles@example.test")
	first, second := f.profile(t, owner, "First"), f.profile(t, owner, "Second")
	f.subscribe(t, first, alpha, "split-a", "Split Creator")
	f.subscribe(t, second, beta, "split-b", "Split Creator")
	if created, err := f.service.AutoGroup(first.Id); err != nil || created != 0 {
		t.Fatalf("channels of two profiles were merged: %d %v", created, err)
	}
}

func TestSubscribingMergesTheCreatorWithoutAskingAndSeparatingIsRemembered(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	owner := f.user(t, "autogroup-flow@example.test")
	profile := f.profile(t, owner, "Flow")
	existing := f.subscribe(t, profile, alpha, "flow-a", "Flow Creator")
	// The second channel is stored first: a subscription is made on a channel record.
	channel := f.channel(t, beta, "flow-b", "Flow Creator")

	created, err := f.service.Create(owner.Id, profile.Id, channel.Id, "")
	creator := created.GetString("creator")
	if err != nil || creator == "" {
		t.Fatalf("the new subscription must come back already grouped: %v %v", created, err)
	}
	if f.creator(t, existing.Id) != creator {
		t.Fatal("the existing subscription was not linked to the new one")
	}
	if name := f.creatorName(t, creator); name != "Flow Creator" {
		t.Fatalf("the creator is named after its first channel: %q", name)
	}

	// Separating deletes the creator and marks each channel, so neither refreshing nor subscribing merges them again.
	if _, err := f.service.Ungroup(owner.Id, profile.Id, creator); err != nil {
		t.Fatal(err)
	}
	if f.creator(t, existing.Id) != "" || f.creator(t, created.Id) != "" || !f.ungrouped(t, existing.Id) || !f.ungrouped(t, created.Id) {
		t.Fatal("separated channels must have no creator and be marked ungrouped")
	}
	if n := f.count(t, creatorsCollection); n != 0 {
		t.Fatalf("the emptied creator was kept: %d", n)
	}
	if grouped, err := f.service.AutoGroup(profile.Id); err != nil || grouped != 0 || f.creator(t, existing.Id) != "" {
		t.Fatalf("a separated creator was merged again: %d %v", grouped, err)
	}
	if again, err := f.service.Create(owner.Id, profile.Id, channel.Id, ""); err != nil || again.GetString("creator") != "" {
		t.Fatalf("subscribing again merged a separated creator: %v %v", again, err)
	}

	// The user can still link them by hand, which lifts the mark.
	if _, err := f.service.Group(owner.Id, profile.Id, []string{existing.Id, created.Id}); err != nil || f.creator(t, existing.Id) == "" || f.creator(t, existing.Id) != f.creator(t, created.Id) {
		t.Fatalf("manual regrouping of separated channels failed: %v", err)
	}
	if f.ungrouped(t, existing.Id) || f.ungrouped(t, created.Id) {
		t.Fatal("grouping by hand kept the ungrouped mark")
	}
}

func TestAutoGroupOfAnUnknownProfileDoesNothing(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	if created, err := f.service.AutoGroup("missing"); err != nil || created != 0 {
		t.Fatalf("created=%d err=%v", created, err)
	}
}
