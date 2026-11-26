package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/EMOBase/emobase-genomics/internal/pkg/entity"
	"github.com/EMOBase/emobase-genomics/internal/pkg/jobpayload"
	ucsequence "github.com/EMOBase/emobase-genomics/internal/pkg/usecase/sequence"
	ucworker "github.com/EMOBase/emobase-genomics/internal/pkg/usecase/worker"
)

// The worker runs a completion hook only if the registered handler satisfies
// OnCompleteHook; a signature drift would silently skip the cleanup.
var (
	_ ucworker.OnCompleteHook = (*SynonymHandler)(nil)
	_ ucworker.OnCompleteHook = (*RNAFNAHandler)(nil)
	_ ucworker.OnCompleteHook = (*CDSFNAHandler)(nil)
	_ ucworker.OnCompleteHook = (*ProteinFAAHandler)(nil)
)

type fakeReplacedFiles struct {
	files    map[string]entity.UploadFile
	replaced map[string][]string
}

func (f fakeReplacedFiles) FindByID(_ context.Context, id string) (*entity.UploadFile, error) {
	if u, ok := f.files[id]; ok {
		return &u, nil
	}
	return nil, nil
}

func (f fakeReplacedFiles) ListIDsReplacedBy(_ context.Context, fileID string) ([]string, error) {
	return f.replaced[fileID], nil
}

type recordingSynonymRepo struct {
	ISynonymRepository
	deletes [][]string
	index   string
}

func (r *recordingSynonymRepo) DeleteByFileIDs(_ context.Context, indexName string, fileIDs []string) error {
	r.deletes, r.index = append(r.deletes, fileIDs), indexName
	return nil
}

type recordingSequenceRepo struct {
	ISequenceRepository
	index, sequenceType string
	ids                 []string
	calls               int
}

func (r *recordingSequenceRepo) DeleteReplaced(_ context.Context, indexName, sequenceType string, ids []string) error {
	r.index, r.sequenceType, r.ids = indexName, sequenceType, ids
	r.calls++
	return nil
}

func jobWith(t *testing.T, payload any) entity.Job {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	msg := json.RawMessage(raw)
	return entity.Job{Payload: &msg}
}

// fakeBundleVersions' version has a zero CreatedAt, which names the index.
var zeroCreated = time.Time{}.Unix()

// A re-uploaded GFF rewrites only the synonyms it still has, so once its
// synonym job is done, the synonyms of the GFF files it replaced must go, from
// that assembly's own synonym index. A species.synonym file replaces nothing:
// all of an assembly's synonym files stay, so its job must delete nothing.
func TestSynonymOnComplete_RemovesOnlyReplacedGFFSynonyms(t *testing.T) {
	files := fakeReplacedFiles{
		files: map[string]entity.UploadFile{
			"gff-new": {ID: "gff-new", FileType: entity.FileTypeGenomicGFF},
			"syn-new": {ID: "syn-new", FileType: entity.FileTypeSpeciesSynonym},
		},
		replaced: map[string][]string{"gff-new": {"gff-old"}, "syn-new": {"syn-old"}},
	}
	repo := &recordingSynonymRepo{}
	h := NewSynonymHandler(fakeBundleVersions{}, nil, repo, files, "p")
	complete := func(fileID string) {
		t.Helper()
		job := jobWith(t, jobpayload.SpeciesSynonymPayload{UploadFileID: fileID, VersionID: 1, AssemblyVersionID: 7})
		if err := h.OnComplete(context.Background(), job, nil); err != nil {
			t.Fatal(err)
		}
	}

	complete("syn-new")
	if len(repo.deletes) != 0 {
		t.Fatalf("a species.synonym upload deleted the synonyms of files %v", repo.deletes)
	}

	complete("gff-new")
	if want := fmt.Sprintf("p-synonym-v1-v1a7-%d", zeroCreated); repo.index != want {
		t.Errorf("deleted from %q, want the assembly's own index %q", repo.index, want)
	}
	if len(repo.deletes) != 1 || !slices.Equal(repo.deletes[0], []string{"gff-old"}) {
		t.Errorf("deleted the synonyms of %v, want [[gff-old]]", repo.deletes)
	}
}

// Once a new RNA file is indexed, the sequences of the files it replaced must
// go, from that assembly's own index and only of its own type: the assembly's
// CDS and protein sequences share the index and stay.
func TestSequenceOnComplete_RemovesReplacedSequencesOfItsType(t *testing.T) {
	files := fakeReplacedFiles{replaced: map[string][]string{"rna-new": {"rna-old"}}}
	repo := &recordingSequenceRepo{}
	h := NewRNAFNAHandler(fakeBundleVersions{}, nil, repo, files, "p")

	job := jobWith(t, jobpayload.ProcessPayload{UploadFileID: "rna-new", VersionID: 1, AssemblyVersionID: 7})
	if err := h.OnComplete(context.Background(), job, nil); err != nil {
		t.Fatal(err)
	}

	if repo.calls != 1 {
		t.Fatalf("DeleteReplaced called %d times, want 1", repo.calls)
	}
	if want := fmt.Sprintf("p-sequence-v1-v1a7-%d", zeroCreated); repo.index != want {
		t.Errorf("deleted from %q, want the assembly's own index %q", repo.index, want)
	}
	if repo.sequenceType != ucsequence.SEQUENCE_TYPE_TRANSCRIPT || !slices.Equal(repo.ids, []string{"rna-old"}) {
		t.Errorf("deleted %s sequences of %v, want TRANSCRIPT of [rna-old]", repo.sequenceType, repo.ids)
	}
}
