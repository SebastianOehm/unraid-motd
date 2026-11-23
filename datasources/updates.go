package datasources

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	log "github.com/sirupsen/logrus"

	"github.com/SebastianOehm/unraid-motd/utils"
)

// Paths of the caches written by Unraid's own update checks
const (
	updatesOSResultFile        = "/tmp/unraidcheck/result.json"
	updatesOSIgnoredFile       = "/tmp/unraidcheck/ignored.json"
	updatesInstalledPluginsDir = "/var/log/plugins"
	updatesLatestPluginsDir    = "/tmp/plugins"
	updatesDockerStatusFile    = "/var/lib/docker/unraid-update-status.json"
	// updatesComposeStatusFile is written by the update check of the Compose Manager Plus plugin
	updatesComposeStatusFile = "/boot/config/plugins/compose.manager/update-status.json"
)

// Container states in the Compose Manager Plus update status that are a result,
// other values mean the plugin could not tell
const (
	composeStatusUpToDate        = "up-to-date"
	composeStatusUpdateAvailable = "update-available"
)

const (
	// updatesDockerTimeout bounds the Docker API calls listing containers.
	updatesDockerTimeout = 5 * time.Second
	// updatesDockerUndef is the status Unraid stores when the update state is unknown.
	updatesDockerUndef = "undef"
	// updatesMaxEntityPasses limits how often nested plugin entities are expanded.
	updatesMaxEntityPasses = 10
	// updatesShaPrefix starts an image reference that is only an image id.
	updatesShaPrefix = "sha256:"
	// updatesShortIDEnd ends the short id cut from an image id.
	updatesShortIDEnd = 19
	// updatesSep is the version separator all version parts are normalized to.
	updatesSep = '.'
	// updatesNumericRank is the rank of a number among the special version forms.
	updatesNumericRank = 4
	// updatesUnknownRank is the rank of a version part that is no known form.
	updatesUnknownRank = -6
)

// Category labels
const (
	updatesLabelOS      = "Unraid OS"
	updatesLabelPlugins = "Plugins"
	updatesLabelDocker  = "Docker"
)

// ErrNotChecked is returned (wrapped in ModuleNotAvailable) when Unraid has not
// written any update check results yet.
var ErrNotChecked = errors.New("no update check results found")

// errNoPluginElement is used when a plugin file has no PLUGIN element.
var errNoPluginElement = errors.New("no PLUGIN element found")

// errNotJSONObject is used when the Docker update status is not a JSON object.
var errNotJSONObject = errors.New("not a JSON object")

var (
	// plgEntityRe matches an entity declaration of a plugin file's DOCTYPE.
	plgEntityRe = regexp.MustCompile(`<!ENTITY\s+(\S+)\s+(?:"([^"]*)"|'([^']*)')\s*>`)
	// plgEntityRefRe matches a reference to an entity.
	plgEntityRefRe = regexp.MustCompile(`&[A-Za-z_][A-Za-z0-9_.\-]*;`)
	// imageRefRe splits an image reference into path, name and tag like Unraid does.
	imageRefRe = regexp.MustCompile(`^(.+/)*([^/:]+)(:[^:/]*)*$`)
	// imageIDRe matches a full image id without the sha256 prefix.
	imageIDRe = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// versionForms are the special version forms of PHP's version_compare with
// their rank, checked in order by prefix.
var versionForms = []struct {
	prefix string
	rank   int
}{
	{"dev", 0},
	{"alpha", 1},
	{"a", 1},
	{"beta", 2},
	{"b", 2},
	{"RC", 3},
	{"rc", 3},
	{"#", updatesNumericRank},
	{"pl", 5},
	{"p", 5},
}

// ConfUpdates extends ConfBase with the update checks to show
type ConfUpdates struct {
	ConfBase `yaml:",inline"`
	// Show pending Unraid OS updates
	CheckOS bool `yaml:"check_os"`
	// Show pending plugin updates
	CheckPlugins bool `yaml:"check_plugins"`
	// Show Docker containers with a pending image update
	CheckDocker bool `yaml:"check_docker"`
	// List of names (plugin, container or "Unraid OS") to ignore
	Ignore []string `yaml:"ignore"`
}

// Init sets default values
func (c *ConfUpdates) Init() {
	c.ConfBase.Init()
	c.CheckOS = true
	c.CheckPlugins = true
	c.CheckDocker = true
	c.Ignore = []string{}
}

// pendingUpdate is one available update
type pendingUpdate struct {
	// Unraid OS, plugin name or container name
	Name string
	// Installed version, empty when unknown (always empty for containers)
	Installed string
	// Available version, empty when unknown (always empty for containers)
	Latest string
}

// updateCategory is the outcome of the update check of one kind of software
type updateCategory struct {
	// Unraid OS, Plugins or Docker
	Label string
	// Cache data was found for this category
	Checked bool
	// Pending updates
	Updates []pendingUpdate
}

// plgInfo holds the attributes of a plugin file's PLUGIN element
type plgInfo struct {
	Name    string
	Version string
	Min     string
	Max     string
}

// dockerUpdateEntry is one image entry of Unraid's Docker update status file
type dockerUpdateEntry struct {
	Local  string `json:"local"`
	Remote string `json:"remote"`
	Status string `json:"status"`
}

// containerImage is a container with the image it was configured with and the
// digests of the image it actually runs
type containerImage struct {
	Name string
	// Image is the configured image of the container
	Image string
	// Digests are the registry digests (sha256:...) of the image the container runs
	Digests []string
}

// GetUpdates reports pending Unraid OS, plugin and Docker image updates. It
// never goes online, it only reads the caches written by Unraid's own checks.
func GetUpdates(channel chan<- SourceReturn, conf *Conf) {
	sourceConf := conf.Updates
	sourceConf.Load(conf)

	returnData := NewSourceReturn(conf.debug)
	defer func() {
		channel <- returnData.Return()
	}()

	var categories []updateCategory

	osVersion, err := readUnraidVersion(unraidVersionFile)
	if err != nil {
		log.Debugf("Could not read the Unraid version: %v", err)
		osVersion = ""
	}

	if sourceConf.CheckOS {
		categories = append(categories, checkOSUpdate(updatesOSResultFile, updatesOSIgnoredFile, osVersion))
	}
	if sourceConf.CheckPlugins {
		categories = append(categories, checkPluginUpdates(updatesInstalledPluginsDir, updatesLatestPluginsDir, osVersion))
	}
	if sourceConf.CheckDocker {
		categories = append(categories, checkDockerUpdates(updatesDockerStatusFile, updatesComposeStatusFile, listContainerImages))
	}

	content, anyChecked := renderUpdates(categories, sourceConf.Ignore, *sourceConf.WarnOnly, sourceConf)
	if !anyChecked {
		t := GetTableWriter(sourceConf)
		returnData.Content = RenderTable(t, "Updates: "+utils.Warn("Not checked"))
		returnData.Error = &ModuleNotAvailable{"updates", ErrNotChecked}

		return
	}

	returnData.Content = content
}

// checkOSUpdate reads the result of Unraid's OS update check. A missing,
// unreadable or failed result counts as not checked.
func checkOSUpdate(resultPath, ignoredPath, currentVersion string) updateCategory {
	category := updateCategory{Label: updatesLabelOS}

	data, err := os.ReadFile(resultPath)
	if err != nil {
		log.Debugf("Could not read OS update result: %v", err)

		return category
	}

	var result struct {
		Version string `json:"version"`
		IsNewer bool   `json:"isNewer"`
		Error   string `json:"error"`
	}
	if err = json.Unmarshal(data, &result); err != nil {
		log.Debugf("Could not parse OS update result: %v", err)

		return category
	}
	if result.Error != "" {
		log.Debugf("OS update check failed: %s", result.Error)

		return category
	}

	category.Checked = true
	if !result.IsNewer || result.Version == "" {
		return category
	}
	ignored := utils.StringSet{}.FromList(readIgnoredReleases(ignoredPath))
	if ignored.Contains(result.Version) {
		return category
	}

	category.Updates = []pendingUpdate{{Name: updatesLabelOS, Installed: currentVersion, Latest: result.Version}}

	return category
}

// readIgnoredReleases returns the OS releases the user chose to ignore in the
// Unraid UI. A missing or broken file means none.
func readIgnoredReleases(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	var ignored struct {
		Releases []string `json:"updateOsIgnoredReleases"`
	}
	if err = json.Unmarshal(data, &ignored); err != nil {
		return nil
	}

	return ignored.Releases
}

// checkPluginUpdates compares the installed plugins with the latest plugin
// files Unraid downloaded. Like Unraid it compares the versions as strings.
func checkPluginUpdates(installedDir, latestDir, osVersion string) updateCategory {
	category := updateCategory{Label: updatesLabelPlugins}

	entries, err := os.ReadDir(installedDir)
	if err != nil {
		log.Debugf("Could not list installed plugins: %v", err)

		return category
	}

	for _, entry := range entries {
		base := entry.Name()
		if !strings.HasSuffix(base, ".plg") || base == "unRAIDServer.plg" || base == "unRAIDServer-.plg" {
			continue
		}

		latest, err := readPlgInfo(filepath.Join(latestDir, base))
		if err != nil {
			continue
		}
		category.Checked = true

		installed, err := readPlgInfo(filepath.Join(installedDir, base))
		if err != nil || installed.Version == "" || latest.Version == "" {
			continue
		}

		if strings.Compare(latest.Version, installed.Version) <= 0 || !pluginCompatible(latest, osVersion) {
			continue
		}

		name := installed.Name
		if name == "" {
			name = strings.TrimSuffix(base, ".plg")
		}
		category.Updates = append(category.Updates, pendingUpdate{Name: name, Installed: installed.Version, Latest: latest.Version})
	}

	sort.Slice(category.Updates, func(i, j int) bool { return category.Updates[i].Name < category.Updates[j].Name })

	return category
}

// pluginCompatible reports whether the Unraid version fits the min and max of
// the plugin. An unknown Unraid version fits everything.
func pluginCompatible(plugin plgInfo, osVersion string) bool {
	if osVersion == "" {
		return true
	}
	if plugin.Min != "" && phpVersionCompare(plugin.Min, osVersion) > 0 {
		return false
	}

	return plugin.Max == "" || phpVersionCompare(plugin.Max, osVersion) >= 0
}

// readPlgInfo reads the attributes of the PLUGIN element of a plugin file. The
// attributes usually reference entities declared in the DOCTYPE.
func readPlgInfo(path string) (plgInfo, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return plgInfo{}, fmt.Errorf("read plugin file: %w", err)
	}

	decoder := xml.NewDecoder(bytes.NewReader(data))
	decoder.Strict = false
	decoder.Entity = parsePlgEntities(string(data))
	decoder.CharsetReader = func(_ string, input io.Reader) (io.Reader, error) { return input, nil }

	for {
		token, err := decoder.Token()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return plgInfo{}, errNoPluginElement
			}

			return plgInfo{}, fmt.Errorf("parse plugin file: %w", err)
		}

		start, ok := token.(xml.StartElement)
		if !ok || !strings.EqualFold(start.Name.Local, "plugin") {
			continue
		}

		return plgInfoFromAttrs(start.Attr), nil
	}
}

// plgInfoFromAttrs picks the plugin attributes
func plgInfoFromAttrs(attrs []xml.Attr) plgInfo {
	var info plgInfo
	for _, attr := range attrs {
		value := strings.TrimSpace(attr.Value)
		switch strings.ToLower(attr.Name.Local) {
		case "name":
			info.Name = value
		case "version":
			info.Version = value
		case "min":
			info.Min = value
		case "max":
			info.Max = value
		}
	}

	return info
}

// parsePlgEntities returns the entities declared in the DOCTYPE of a plugin
// file, with references between them expanded.
func parsePlgEntities(data string) map[string]string {
	entities := map[string]string{}
	for _, m := range plgEntityRe.FindAllStringSubmatch(data, -1) {
		value := m[2]
		if value == "" {
			value = m[3]
		}
		entities[m[1]] = value
	}

	// Entities may reference each other, the limit guards against cycles
	for range updatesMaxEntityPasses {
		changed := false
		for name, value := range entities {
			expanded := plgEntityRefRe.ReplaceAllStringFunc(value, func(ref string) string {
				target := ref[1 : len(ref)-1]
				if replacement, ok := entities[target]; ok && target != name {
					return replacement
				}

				return ref
			})
			if expanded != value {
				entities[name] = expanded
				changed = true
			}
		}
		if !changed {
			break
		}
	}

	return entities
}

// phpVersionCompare compares two version strings like PHP's version_compare and
// returns -1, 0 or 1.
func phpVersionCompare(a, b string) int {
	partsA, partsB := canonVersion(a), canonVersion(b)

	idx := 0
	for ; idx < len(partsA) && idx < len(partsB); idx++ {
		if result := compareVersionPart(partsA[idx], partsB[idx]); result != 0 {
			return result
		}
	}

	switch {
	case idx < len(partsA):
		return compareLeftoverPart(partsA[idx])
	case idx < len(partsB):
		return -compareLeftoverPart(partsB[idx])
	default:
		return 0
	}
}

// canonVersion splits a version into its parts: _, - and + become separators and
// a separator is inserted between digits and non-digits.
func canonVersion(version string) []string {
	var builder strings.Builder
	var prev rune
	for pos, char := range version {
		switch char {
		case '_', '-', '+':
			char = updatesSep
		}
		if pos > 0 && char != updatesSep && prev != updatesSep && isDigit(char) != isDigit(prev) {
			builder.WriteRune(updatesSep)
		}
		builder.WriteRune(char)
		prev = char
	}

	return strings.FieldsFunc(builder.String(), func(r rune) bool { return r == updatesSep })
}

// isDigit reports whether char is an ASCII digit
func isDigit(char rune) bool {
	return char >= '0' && char <= '9'
}

// compareVersionPart compares two parts of a canonical version
func compareVersionPart(left, right string) int {
	leftNum, rightNum := isDigit(rune(left[0])), isDigit(rune(right[0]))

	switch {
	case leftNum && rightNum:
		return compareNumeric(left, right)
	case leftNum:
		return sign(updatesNumericRank - versionFormRank(right))
	case rightNum:
		return sign(versionFormRank(left) - updatesNumericRank)
	default:
		return sign(versionFormRank(left) - versionFormRank(right))
	}
}

// compareLeftoverPart rates the first unmatched part of the longer version
// against the shorter one: a number makes it greater, a form like beta or pl
// is ranked against a number.
func compareLeftoverPart(part string) int {
	if isDigit(rune(part[0])) {
		return 1
	}

	return sign(versionFormRank(part) - updatesNumericRank)
}

// versionFormRank returns the rank of a non-numeric version part
func versionFormRank(part string) int {
	for _, form := range versionForms {
		if strings.HasPrefix(part, form.prefix) {
			return form.rank
		}
	}

	return updatesUnknownRank
}

// compareNumeric compares two numbers of any length given as digit strings
func compareNumeric(left, right string) int {
	left, right = strings.TrimLeft(left, "0"), strings.TrimLeft(right, "0")
	if len(left) != len(right) {
		return sign(len(left) - len(right))
	}

	return strings.Compare(left, right)
}

// sign returns -1, 0 or 1 for the sign of value
func sign(value int) int {
	switch {
	case value < 0:
		return -1
	case value > 0:
		return 1
	default:
		return 0
	}
}

// checkDockerUpdates reports the containers with a pending image update. A
// container checked by Compose Manager Plus takes that result, all others are
// judged by the update status Unraid keeps for Docker images. Either file is
// enough for the category to count as checked.
func checkDockerUpdates(statusPath, composeStatusPath string, list func() ([]containerImage, error)) updateCategory {
	category := updateCategory{Label: updatesLabelDocker}

	status, statusErr := readDockerUpdateStatus(statusPath)
	if statusErr != nil {
		log.Debugf("Could not read Docker update status: %v", statusErr)
	}
	compose, composeErr := readComposeUpdateStatus(composeStatusPath)
	if composeErr != nil {
		log.Debugf("Could not read Compose Manager Plus update status: %v", composeErr)
	}
	if statusErr != nil && composeErr != nil {
		return category
	}

	containers, err := list()
	if err != nil {
		log.Debugf("Could not list containers: %v", err)

		return category
	}

	category.Checked = true
	var others []containerImage
	for _, ctr := range containers {
		hasUpdate, checked := compose[ctr.Name]
		if !checked {
			others = append(others, ctr)

			continue
		}
		if hasUpdate {
			category.Updates = append(category.Updates, pendingUpdate{Name: ctr.Name})
		}
	}
	category.Updates = append(category.Updates, dockerUpdates(others, status)...)
	sort.Slice(category.Updates, func(i, j int) bool { return category.Updates[i].Name < category.Updates[j].Name })

	return category
}

// readComposeUpdateStatus reads the result of the Compose Manager Plus update
// check. It maps the names of the containers the plugin could check to whether
// an update is pending. Containers it could not check are left out, so the
// Unraid status still applies to them.
func readComposeUpdateStatus(path string) (map[string]bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read compose update status: %w", err)
	}

	var file struct {
		Stacks map[string]struct {
			Containers []struct {
				Name         string `json:"name"`
				UpdateStatus string `json:"updateStatus"`
			} `json:"containers"`
		} `json:"stacks"`
	}
	if err = json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parse compose update status: %w", err)
	}

	result := map[string]bool{}
	for _, stack := range file.Stacks {
		for _, ctr := range stack.Containers {
			switch ctr.UpdateStatus {
			case composeStatusUpToDate, composeStatusUpdateAvailable:
				result[ctr.Name] = ctr.UpdateStatus == composeStatusUpdateAvailable
			}
		}
	}

	return result, nil
}

// readDockerUpdateStatus reads Unraid's Docker update status file, which maps
// image references to their local and remote digests. Entries that cannot be
// decoded are skipped.
func readDockerUpdateStatus(path string) (map[string]dockerUpdateEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read docker update status: %w", err)
	}

	var raw map[string]json.RawMessage
	if err = json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse docker update status: %w", err)
	}
	if raw == nil {
		return nil, errNotJSONObject
	}

	status := make(map[string]dockerUpdateEntry, len(raw))
	for image, value := range raw {
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			continue
		}

		var entry dockerUpdateEntry
		if err = json.Unmarshal(value, &entry); err != nil {
			continue
		}
		status[image] = entry
	}

	return status, nil
}

// ensureImageTag normalizes an image reference to repository:tag the way
// Unraid does to look it up in the update status.
func ensureImageTag(image string) string {
	repo, tag := splitImage(image)
	if tag == "" {
		tag = "latest"
	}

	return strings.TrimSpace(repo) + ":" + strings.TrimSpace(tag)
}

// splitImage splits an image reference into repository and tag, the tag is
// empty when the reference has none.
func splitImage(image string) (repo, tag string) {
	if strings.HasPrefix(image, updatesShaPrefix) {
		// Only an image id, Unraid names it after the short id
		return image[len(updatesShaPrefix):min(len(image), updatesShortIDEnd)], "latest"
	}

	if !strings.Contains(image, "/") {
		image = "library/" + image
	}

	m := imageRefRe.FindStringSubmatch(image)
	if m == nil {
		return image, ""
	}

	return m[1] + m[2], strings.TrimPrefix(m[3], ":")
}

// dockerUpdates returns the containers with a pending image update. It follows
// Unraid's own rule: an update is pending when the stored local digest differs
// from the remote digest of Unraid's last update check. The exception are
// containers that already run the remote digest, because Unraid only refreshes
// the stored local digest when its own Docker manager updates a container, so
// it goes stale after an update by other means such as docker compose.
func dockerUpdates(containers []containerImage, status map[string]dockerUpdateEntry) []pendingUpdate {
	var updates []pendingUpdate

	for _, ctr := range containers {
		entry, ok := status[ensureImageTag(ctr.Image)]
		if !ok || entry.Status == updatesDockerUndef {
			continue
		}

		local, remote := normalizeDigest(entry.Local), normalizeDigest(entry.Remote)
		if local == "" || remote == "" || local == remote || digestsContain(ctr.Digests, remote) {
			continue
		}
		updates = append(updates, pendingUpdate{Name: ctr.Name})
	}

	sort.Slice(updates, func(i, j int) bool { return updates[i].Name < updates[j].Name })

	return updates
}

// normalizeDigest trims and lowercases a digest, the value "undef" Unraid stores
// for an unknown digest counts as empty.
func normalizeDigest(digest string) string {
	digest = strings.ToLower(strings.TrimSpace(digest))
	if digest == updatesDockerUndef {
		return ""
	}

	return digest
}

// digestsContain reports whether digest, already trimmed and lowercased, is one
// of digests.
func digestsContain(digests []string, digest string) bool {
	for _, candidate := range digests {
		if strings.ToLower(strings.TrimSpace(candidate)) == digest {
			return true
		}
	}

	return false
}

// listContainerImages lists all containers with the image they were configured
// with and the digests of the image they run.
func listContainerImages() ([]containerImage, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithVersion(dockerMinAPI))
	if err != nil {
		return nil, fmt.Errorf("docker client: %w", err)
	}
	defer cli.Close()

	ctx, cancel := context.WithTimeout(context.Background(), updatesDockerTimeout)
	defer cancel()

	list, err := cli.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return nil, fmt.Errorf("docker container list: %w", err)
	}

	digestCache := map[string][]string{}
	containers := make([]containerImage, 0, len(list))
	for _, item := range list {
		image := item.Image
		if strings.HasPrefix(image, updatesShaPrefix) || imageIDRe.MatchString(image) {
			// The list shows the id once the image tag moved on, the container still knows its configured image
			if inspected, inspectErr := cli.ContainerInspect(ctx, item.ID); inspectErr == nil && inspected.Config != nil {
				image = inspected.Config.Image
			}
		}

		digests, cached := digestCache[item.ImageID]
		if !cached {
			digests = imageDigests(ctx, cli, item.ImageID)
			digestCache[item.ImageID] = digests
		}

		containers = append(containers, containerImage{Name: dockerContainerName(item), Image: image, Digests: digests})
	}

	return containers, nil
}

// imageDigests returns the registry digests of a local image, nil when the
// image cannot be inspected.
func imageDigests(ctx context.Context, cli *client.Client, imageID string) []string {
	inspected, _, err := cli.ImageInspectWithRaw(ctx, imageID)
	if err != nil {
		log.Debugf("Could not inspect image %s: %v", imageID, err)

		return nil
	}

	var digests []string
	for _, repoDigest := range inspected.RepoDigests {
		if _, digest, found := strings.Cut(repoDigest, "@"); found {
			digests = append(digests, digest)
		}
	}

	return digests
}

// renderUpdates renders the pending updates of all checked categories. The
// second return value is false when no category had check results, the content
// is empty then.
func renderUpdates(categories []updateCategory, ignore []string, warnOnly bool, tableConf TableConfig) (content string, anyChecked bool) {
	outputTable := GetTableWriter(tableConf)
	ignored := utils.StringSet{}.FromList(ignore)
	anyUpdate := false

	for _, category := range categories {
		if !category.Checked {
			continue
		}
		anyChecked = true

		rows := 0
		for _, update := range category.Updates {
			if ignored.Contains(update.Name) {
				continue
			}
			outputTable.AppendRow([]interface{}{update.Name, utils.Warn(updateText(update))})
			rows++
		}

		anyUpdate = anyUpdate || rows > 0
		if rows == 0 && !warnOnly {
			outputTable.AppendRow([]interface{}{category.Label, utils.Good("up to date")})
		}
	}

	if !anyChecked {
		return "", false
	}

	title := "Updates: " + utils.Good("OK")
	if anyUpdate {
		title = "Updates: " + utils.Warn("Warning")
	}

	return RenderTable(outputTable, title), true
}

// updateText describes a pending update
func updateText(update pendingUpdate) string {
	switch {
	case update.Installed != "" && update.Latest != "":
		return update.Installed + " → " + update.Latest
	case update.Latest != "":
		return update.Latest + " available"
	default:
		return "update available"
	}
}
