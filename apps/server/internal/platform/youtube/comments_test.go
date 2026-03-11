package youtube

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// commentsScript answers by video id; the author depends on whose cookies the call carries.
const commentsScript = `
cookie=
previous=
for arg in "$@"; do
  if [ "$previous" = '--cookies' ]; then cookie=$arg; fi
  previous=$arg
done
case "$*" in
 *'watch?v=error000001'*) echo 'secret-cookie /tmp/private-cookies downloader error' >&2; exit 1 ;;
 *'watch?v=badjson0001'*) printf '%s\n' '{bad json'; exit 0 ;;
 *'watch?v=trailing001'*) printf '%s\n' '{"id":"trailing001","comments":[]}' 'extra'; exit 0 ;;
 *'watch?v=empty000001'*) printf '%s\n' '{"id":"empty000001","comments":[]}'; exit 0 ;;
 *'watch?v=otherid0001'*) printf '%s\n' '{"id":"someoneelse","comments":[]}'; exit 0 ;;
 *'watch?v=noid0000001'*) printf '%s\n' '{"id":"noid0000001","comments":[{"author":"A"}]}'; exit 0 ;;
 *'watch?v=denied00001'*) echo 'sign in secret-cookie /tmp/private-cookies' >&2; exit 1 ;;
 *'watch?v=replies0001'*) printf '%s\n' '{"id":"replies0001","comments":[{"id":"a","author":"A","text":"one","parent":"root"},{"id":"r1","author":"R","text":"first","parent":"a"},{"id":"b","author":"B","text":"two","parent":"root"},{"id":"r2","author":"R","text":"second","parent":"a"},{"id":"orphan","author":"O","text":"lost","parent":"missing"},{"id":"c","author":"C","text":"three","parent":"root"}]}'; exit 0 ;;
esac
if [ -n "$cookie" ]; then author=Private; else author=Public; fi
printf '{"id":"dQw4w9WgXcQ","comments":[{"id":"comment1","author":"%s","text":"hello","timestamp":1700000000,"like_count":12,"parent":"root"},{"id":"reply1","author":"Reply","text":"reply","parent":"comment1"}]}\n' "$author"
`

func TestCommentsNestRepliesUnderTheirParent(t *testing.T) {
	t.Parallel()
	p, _ := fakeYTDLP(t, commentsScript)
	comments, err := p.Comments(context.Background(), "dQw4w9WgXcQ", "")
	if err != nil || len(comments) != 1 {
		t.Fatalf("comments=%+v err=%v", comments, err)
	}
	comment := comments[0]
	if comment.ID != "comment1" || comment.Author != "Public" || comment.Text != "hello" || comment.PublishedAt != "2023-11-14T22:13:20Z" || comment.LikeCount != 12 || len(comment.Replies) != 1 {
		t.Fatalf("comment: %+v", comment)
	}
	if reply := comment.Replies[0]; reply.ID != "reply1" || reply.Author != "Reply" || reply.Text != "reply" || len(reply.Replies) != 0 {
		t.Fatalf("reply: %+v", reply)
	}
}

func TestCommentRepliesKeepYouTubesOrderAndOrphansAreDropped(t *testing.T) {
	t.Parallel()
	p, _ := fakeYTDLP(t, commentsScript)
	comments, err := p.Comments(context.Background(), "replies0001", "")
	if err != nil || len(comments) != 3 {
		t.Fatalf("comments=%+v err=%v", comments, err)
	}
	first := comments[0]
	if first.ID != "a" || len(first.Replies) != 2 || first.Replies[0].ID != "r1" || first.Replies[0].Text != "first" || first.Replies[1].ID != "r2" {
		t.Fatalf("replies keep YouTube's order under their parent: %+v", first.Replies)
	}
	if comments[1].ID != "b" || len(comments[1].Replies) != 0 || comments[2].ID != "c" {
		t.Fatalf("a thread without replies carries none: %+v", comments[1:])
	}
	for _, comment := range comments {
		if comment.ID == "orphan" {
			t.Fatal("an orphan reply became a top-level comment")
		}
	}
}

func TestCommentsOfAVideoWithoutAnyAreAnEmptyList(t *testing.T) {
	t.Parallel()
	p, _ := fakeYTDLP(t, commentsScript)
	comments, err := p.Comments(context.Background(), "empty000001", "")
	if err != nil || comments == nil || len(comments) != 0 {
		t.Fatalf("comments=%#v err=%v", comments, err)
	}
}

func TestCommentsAreBoundedToOneHundredThreads(t *testing.T) {
	t.Parallel()
	var items []string
	for index := range 130 {
		items = append(items, fmt.Sprintf(`{"id":"c%d","author":"A","parent":"root"}`, index))
	}
	p, _ := fakeYTDLP(t, `printf '%s\n' '{"id":"many0000001","comments":[`+strings.Join(items, ",")+`]}'`)
	comments, err := p.Comments(context.Background(), "many0000001", "")
	if err != nil || len(comments) != 100 {
		t.Fatalf("got %d comments, err=%v", len(comments), err)
	}
}

func TestCommentsFailuresAreSafeErrors(t *testing.T) {
	t.Parallel()
	p, _ := fakeYTDLP(t, commentsScript)
	for _, id := range []string{"error000001", "badjson0001", "trailing001", "otherid0001", "noid0000001"} {
		_, err := p.Comments(context.Background(), id, "")
		assertCode(t, err, "SOURCE_REQUEST_FAILED")
	}
	// A public request that is asked to sign in is not a session problem: there was no session.
	_, err := p.Comments(context.Background(), "denied00001", "")
	assertCode(t, err, "SOURCE_REQUEST_FAILED")
	// A connected one is.
	_, err = p.Comments(context.Background(), "denied00001", "/private/jar.txt")
	assertCode(t, err, "SOURCE_SESSION_INVALID")
}

func TestCommentsRejectUnsafeIdsWithoutRunningAnything(t *testing.T) {
	t.Parallel()
	p, log := fakeYTDLP(t, commentsScript)
	for _, id := range []string{"http:127.0.0.1", "-proxy", "", "dQw4w9WgXcQ&x=1"} {
		_, err := p.Comments(context.Background(), id, "")
		assertCode(t, err, "INVALID_SOURCE_INPUT")
	}
	if got := calls(t, log); got != "" {
		t.Fatalf("an invalid id started yt-dlp: %s", got)
	}
}

func TestConnectedCommentsUseTheSessionJar(t *testing.T) {
	t.Parallel()
	p, log := fakeYTDLP(t, commentsScript)
	jar := filepath.Join(t.TempDir(), "jar.txt")
	if err := os.WriteFile(jar, []byte("# Netscape HTTP Cookie File\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	comments, err := p.Comments(context.Background(), "dQw4w9WgXcQ", jar)
	if err != nil || len(comments) != 1 || comments[0].Author != "Private" {
		t.Fatalf("comments=%+v err=%v", comments, err)
	}
	args := calls(t, log)
	if !strings.Contains(args, "--cookies "+jar) || !strings.Contains(args, "--get-comments --extractor-args youtube:max_comments=150,100,50,5,1 https://www.youtube.com/watch?v=dQw4w9WgXcQ") {
		t.Fatalf("arguments: %s", args)
	}
}
