package gitlab

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gitlab "gitlab.com/gitlab-org/api/client-go"
	"golang.org/x/sync/errgroup"

	"github.com/trufflesecurity/trufflehog/v3/pkg/context"
	"github.com/trufflesecurity/trufflehog/v3/pkg/sources"
	"github.com/trufflesecurity/trufflehog/v3/pkg/sources/git"
)

func Test_setProgressCompleteWithRepo_resumeInfo(t *testing.T) {
	tests := []struct {
		startingResumeInfoSlice []string
		repoURL                 string
		wantResumeInfoSlice     []string
	}{
		{
			startingResumeInfoSlice: []string{},
			repoURL:                 "a",
			wantResumeInfoSlice:     []string{"a"},
		},
		{
			startingResumeInfoSlice: []string{"b"},
			repoURL:                 "a",
			wantResumeInfoSlice:     []string{"a", "b"},
		},
	}

	s := &Source{repos: []string{}}

	for _, tt := range tests {
		s.resumeInfoSlice = tt.startingResumeInfoSlice
		s.setProgressCompleteWithRepo(0, 0, tt.repoURL)
		if !reflect.DeepEqual(s.resumeInfoSlice, tt.wantResumeInfoSlice) {
			t.Errorf("s.setProgressCompleteWithRepo() got: %v, want: %v", s.resumeInfoSlice, tt.wantResumeInfoSlice)
		}
	}
}

func Test_setProgressCompleteWithRepo_Progress(t *testing.T) {
	repos := []string{"a", "b", "c", "d", "e"}
	tests := map[string]struct {
		repos                 []string
		index                 int
		offset                int
		wantPercentComplete   int64
		wantSectionsCompleted int32
		wantSectionsRemaining int32
	}{
		"starting from the beginning, no offset": {
			repos:                 repos,
			index:                 0,
			offset:                0,
			wantPercentComplete:   0,
			wantSectionsCompleted: 0,
			wantSectionsRemaining: 5,
		},
		"resume from the third, offset 2": {
			repos:                 repos[2:],
			index:                 0,
			offset:                2,
			wantPercentComplete:   40,
			wantSectionsCompleted: 2,
			wantSectionsRemaining: 5,
		},
		"resume from the third, on last repo, offset 2": {
			repos:                 repos[2:],
			index:                 2,
			offset:                2,
			wantPercentComplete:   80,
			wantSectionsCompleted: 4,
			wantSectionsRemaining: 5,
		},
	}

	for _, tt := range tests {
		s := &Source{
			repos: tt.repos,
		}

		s.setProgressCompleteWithRepo(tt.index, tt.offset, "")
		gotProgress := s.GetProgress()
		if gotProgress.PercentComplete != tt.wantPercentComplete {
			t.Errorf("s.setProgressCompleteWithRepo() PercentComplete got: %v want: %v", gotProgress.PercentComplete, tt.wantPercentComplete)
		}
		if gotProgress.SectionsCompleted != tt.wantSectionsCompleted {
			t.Errorf("s.setProgressCompleteWithRepo() PercentComplete got: %v want: %v", gotProgress.SectionsCompleted, tt.wantSectionsCompleted)
		}
		if gotProgress.SectionsRemaining != tt.wantSectionsRemaining {
			t.Errorf("s.setProgressCompleteWithRepo() PercentComplete got: %v want: %v", gotProgress.SectionsRemaining, tt.wantSectionsRemaining)
		}
	}
}

func Test_scanRepos_SetProgressComplete(t *testing.T) {
	testCases := []struct {
		name         string
		repos        []string
		wantComplete bool
		wantErr      bool
	}{
		{
			name:         "no repos",
			wantComplete: true,
		},
		{
			name:         "one valid repo",
			repos:        []string{"repo"},
			wantComplete: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			src := &Source{
				repos: tc.repos,
			}
			src.jobPool = &errgroup.Group{}
			src.scanOptions = &git.ScanOptions{}

			_ = src.scanRepos(context.Background(), nil)
			if !tc.wantErr {
				assert.Equal(t, "", src.GetProgress().EncodedResumeInfo)
			}

			gotComplete := src.GetProgress().PercentComplete == 100
			if gotComplete != tc.wantComplete {
				t.Errorf("got: %v, want: %v", gotComplete, tc.wantComplete)
			}
		})
	}
}

func Test_normalizeGitlabEndpoint(t *testing.T) {
	testCases := map[string]struct {
		inputEndpoint  string
		outputEndpoint string
		wantErr        bool
	}{
		"the cloud url should return the cloud url": {
			inputEndpoint:  gitlabCloudBaseURL,
			outputEndpoint: gitlabCloudBaseURL,
		},
		"empty string should return the cloud url": {
			inputEndpoint:  "",
			outputEndpoint: gitlabCloudBaseURL,
		},
		"no scheme cloud url should return the cloud url": {
			inputEndpoint:  "gitlab.com",
			outputEndpoint: gitlabCloudBaseURL,
		},
		"no scheme cloud url with trailing slash should return the cloud url": {
			inputEndpoint:  "gitlab.com/",
			outputEndpoint: gitlabCloudBaseURL,
		},
		"http scheme cloud url with organization should return the cloud url": {
			inputEndpoint:  "http://gitlab.com/trufflesec",
			outputEndpoint: gitlabCloudBaseURL,
		},
		// On-prem endpoint testing.
		"on-prem url should be unchanged": {
			inputEndpoint:  "https://gitlab.trufflesec.com/",
			outputEndpoint: "https://gitlab.trufflesec.com/",
		},
		"on-prem url without trailing slash should have trailing slash added": {
			inputEndpoint:  "https://gitlab.trufflesec.com",
			outputEndpoint: "https://gitlab.trufflesec.com/",
		},
		"on-prem url with http scheme should return an error": {
			inputEndpoint: "http://gitlab.trufflesec.com/",
			wantErr:       true,
		},
		"on-prem with gitlab.com should not rewrite to the cloud url": {
			inputEndpoint:  "https://gitlab.com.trufflesec.com/",
			outputEndpoint: "https://gitlab.com.trufflesec.com/",
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			output, err := normalizeGitlabEndpoint(tc.inputEndpoint)
			assert.Equal(t, tc.outputEndpoint, output)
			assert.Equal(t, tc.wantErr, err != nil)
		})
	}
}

func TestEnumerationSkipsProjectsWithoutRepositories(t *testing.T) {
	projects := []*gitlab.Project{
		{ID: 1, HTTPURLToRepo: "https://gitlab.example.invalid/group/disabled.git", RepositoryAccessLevel: gitlab.DisabledAccessControl},
		{ID: 2, HTTPURLToRepo: ""},
		{ID: 3, HTTPURLToRepo: "https://gitlab.example.invalid/group/enabled.git", RepositoryAccessLevel: gitlab.EnabledAccessControl},
		{ID: 4, HTTPURLToRepo: "https://gitlab.example.invalid/group/private.git", RepositoryAccessLevel: gitlab.PrivateAccessControl},
		// Simplified API responses may omit repository_access_level.
		{ID: 5, HTTPURLToRepo: "https://gitlab.example.invalid/group/available.git"},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var response any
		switch {
		case r.URL.Path == "/api/v4/user":
			response = map[string]any{"id": 1}
		case r.URL.Path == "/api/v4/groups":
			response = []map[string]any{{"id": 10}}
		case strings.HasSuffix(r.URL.Path, "/projects"):
			response = projects
			if r.URL.Query().Get("simple") == "true" {
				basic := make([]map[string]any, 0, len(projects))
				for _, project := range projects {
					basic = append(basic, map[string]any{
						"id": project.ID, "http_url_to_repo": project.HTTPURLToRepo,
					})
				}
				response = basic
			}
		default:
			http.NotFound(w, r)
			return
		}
		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(server.Close)
	client, err := gitlab.NewClient("offline-fixture", gitlab.WithBaseURL(server.URL))
	require.NoError(t, err)

	for _, mode := range []string{"legacy", "simplified", "group"} {
		t.Run(mode, func(t *testing.T) {
			source := &Source{
				url: server.URL, projectsPerPage: 100, groupIds: []string{"10"},
				projectMetadataCache: expirable.NewLRU[string, *projectMetadata](8, nil, 0),
			}
			var units []string
			var enumerationErrors []error
			reporter := sources.VisitorReporter{
				VisitUnit: func(_ context.Context, unit sources.SourceUnit) error {
					id, _ := unit.SourceUnitID()
					units = append(units, id)
					return nil
				},
				VisitErr: func(_ context.Context, err error) error {
					enumerationErrors = append(enumerationErrors, err)
					return nil
				},
			}
			ignore := func(string) bool { return false }
			switch mode {
			case "legacy":
				err = source.getAllProjectRepos(context.Background(), client, ignore, reporter)
			case "simplified":
				err = source.getAllProjectReposV2(context.Background(), client, ignore, reporter)
			case "group":
				err = source.getAllProjectReposInGroups(context.Background(), client, ignore, reporter)
			}
			require.NoError(t, err)
			require.Empty(t, enumerationErrors)
			require.ElementsMatch(t, []string{
				projects[2].HTTPURLToRepo, projects[3].HTTPURLToRepo, projects[4].HTTPURLToRepo,
			}, units)
		})
	}
}
