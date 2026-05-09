//go:build windows

package keychain

import (
	"errors"
	"os/exec"
	"strings"
	"syscall"
)

// ErrPromptCancelled is returned when the user dismisses the dialog
// without entering a password (Cancel button or empty input).
var ErrPromptCancelled = errors.New("keychain: prompt cancelled")

// promptScript renders a small WinForms password dialog. Output is the
// plain-text password on stdout, or empty on cancel.
const promptScript = `
$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Windows.Forms
Add-Type -AssemblyName System.Drawing

$form = New-Object System.Windows.Forms.Form
$form.Text = $env:NB_PROMPT_TITLE
$form.Size = New-Object System.Drawing.Size(420,170)
$form.StartPosition = 'CenterScreen'
$form.FormBorderStyle = 'FixedDialog'
$form.MaximizeBox = $false
$form.MinimizeBox = $false
$form.TopMost = $true

$label = New-Object System.Windows.Forms.Label
$label.Location = New-Object System.Drawing.Point(12,15)
$label.Size = New-Object System.Drawing.Size(390,30)
$label.Text = $env:NB_PROMPT_MESSAGE
$form.Controls.Add($label)

$txt = New-Object System.Windows.Forms.TextBox
$txt.Location = New-Object System.Drawing.Point(12,50)
$txt.Size = New-Object System.Drawing.Size(390,20)
$txt.UseSystemPasswordChar = $true
$form.Controls.Add($txt)

$ok = New-Object System.Windows.Forms.Button
$ok.Location = New-Object System.Drawing.Point(245,90)
$ok.Size = New-Object System.Drawing.Size(75,25)
$ok.Text = 'OK'
$ok.DialogResult = [System.Windows.Forms.DialogResult]::OK
$form.AcceptButton = $ok
$form.Controls.Add($ok)

$cancel = New-Object System.Windows.Forms.Button
$cancel.Location = New-Object System.Drawing.Point(327,90)
$cancel.Size = New-Object System.Drawing.Size(75,25)
$cancel.Text = 'Cancel'
$cancel.DialogResult = [System.Windows.Forms.DialogResult]::Cancel
$form.CancelButton = $cancel
$form.Controls.Add($cancel)

$form.Add_Shown({ $txt.Focus() | Out-Null })
$result = $form.ShowDialog()
if ($result -eq [System.Windows.Forms.DialogResult]::OK) {
    [Console]::Out.Write($txt.Text)
    exit 0
}
exit 1
`

// runPromptScript invokes PowerShell with the password prompt script,
// passing the title and message via env vars. Factored into a
// package-level var so tests can stub the system call instead of
// firing a real WinForms dialog.
var runPromptScript = func(title, message string) ([]byte, error) {
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive",
		"-WindowStyle", "Hidden", "-Command", promptScript)
	cmd.Env = append(cmd.Env,
		"NB_PROMPT_TITLE="+title,
		"NB_PROMPT_MESSAGE="+message,
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd.Output()
}

// PromptDialog shows a native password dialog and returns the entered
// password. Returns ErrPromptCancelled on cancel or empty input.
func PromptDialog(title, message string) (string, error) {
	out, err := runPromptScript(title, message)
	if err != nil {
		// Non-zero exit (Cancel button pressed) or any other failure.
		return "", ErrPromptCancelled
	}
	pw := strings.TrimRight(string(out), "\r\n")
	if pw == "" {
		return "", ErrPromptCancelled
	}
	return pw, nil
}
