<?php
// AJAX backend for the MOTD settings page. Only fixed paths are written.
const MOTD_BIN = '/usr/local/bin/motd';
const MOTD_DIR = '/boot/config/plugins/motd';
const MOTD_CFG = '/boot/config/plugins/motd/config.yaml';

header('Content-Type: application/json');

function motd_reply(array $data, int $code = 200): void {
    http_response_code($code);
    echo json_encode($data);
    exit;
}

function motd_fail(string $msg, int $code = 200): void {
    motd_reply(['ok' => false, 'error' => $msg], $code);
}

function motd_run(string $args): array {
    $out = [];
    $rc = 0;
    exec('timeout 20 ' . escapeshellarg(MOTD_BIN) . ' ' . $args . ' 2>&1', $out, $rc);
    return [implode("\n", $out), $rc];
}

if (($_SERVER['REQUEST_METHOD'] ?? '') !== 'POST') {
    motd_fail('Method not allowed', 405);
}
// CSRF: emhttp's local_prepend.php validates csrf_token on every POST and removes it from $_POST

$action = $_POST['action'] ?? '';
if (!in_array($action, ['preview', 'save', 'defaults'], true)) {
    motd_fail('Unknown action');
}
$config = $_POST['config'] ?? '';
if (!is_string($config)) {
    motd_fail('Invalid config');
}
if (strlen($config) > 65536) {
    motd_fail('Config too large');
}
$config = str_replace(["\r\n", "\r"], "\n", $config);

if (!is_file(MOTD_BIN) || !is_executable(MOTD_BIN)) {
    motd_fail('motd binary not found or not executable');
}

if ($action === 'defaults') {
    $out = [];
    $rc = 0;
    exec(escapeshellarg(MOTD_BIN) . ' --dump-config -c /nonexistent 2>/dev/null', $out, $rc);
    if ($rc !== 0) {
        motd_fail('Could not get defaults');
    }
    motd_reply(['ok' => true, 'output' => implode("\n", $out) . "\n"]);
}

$tmp = tempnam(sys_get_temp_dir(), 'motd');
if ($tmp === false || file_put_contents($tmp, $config) === false) {
    if ($tmp !== false) {
        @unlink($tmp);
    }
    motd_fail('Could not write temporary file');
}

$t = escapeshellarg($tmp);
if ($action === 'preview') {
    [$output, $rc] = motd_run("--preview --width 100 -c $t");
    if ($rc === 124) {
        $result = ['ok' => false, 'error' => 'Preview timed out', 'output' => $output];
    } else {
        $result = ['ok' => true, 'output' => $output];
    }
} else { // save
    [$output, $rc] = motd_run("--check-config -c $t");
    if ($rc !== 0) {
        $result = ['ok' => false, 'error' => $output !== '' ? $output : 'Invalid config'];
    } elseif (!is_dir(MOTD_DIR) && !mkdir(MOTD_DIR, 0755, true) && !is_dir(MOTD_DIR)) {
        $result = ['ok' => false, 'error' => 'Could not create ' . MOTD_DIR];
    } elseif (!copy($tmp, MOTD_CFG)) { // copy, not rename: /boot is a different filesystem (FAT)
        $result = ['ok' => false, 'error' => 'Could not write ' . MOTD_CFG];
    } else {
        $result = ['ok' => true];
    }
}
@unlink($tmp);
motd_reply($result);
