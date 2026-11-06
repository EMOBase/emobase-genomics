package sequence

import (
	"context"
	"strings"
	"testing"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
)

type captureRepo struct{ seqs []entity.Sequence }

func (c *captureRepo) SaveMany(_ context.Context, _ string, seqs []entity.Sequence) error {
	c.seqs = append(c.seqs, seqs...)
	return nil
}

// Every sequence must carry the upload it came from: that is how a replaced
// file's sequences are found and removed once a new file of its type is
// indexed into the assembly's shared sequence index.
func TestLoad_TagsSequencesWithTheirFile(t *testing.T) {
	repo := &captureRepo{}
	fasta := ">tx1\nACGT\n>tx2\nGGCC\n"
	if err := New(repo, 10).Load(context.Background(), strings.NewReader(fasta), "idx", SEQUENCE_TYPE_TRANSCRIPT, "Tcas", "file-1"); err != nil {
		t.Fatal(err)
	}
	if len(repo.seqs) != 2 {
		t.Fatalf("indexed %d sequences, want 2", len(repo.seqs))
	}
	for _, s := range repo.seqs {
		if s.FileID != "file-1" {
			t.Errorf("sequence %q has file_id %q, want file-1", s.Name, s.FileID)
		}
	}
}
