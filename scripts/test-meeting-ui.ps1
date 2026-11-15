$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$repo = Split-Path -Parent $PSScriptRoot
$exe = Join-Path $repo 'voice-rewrite/VoiceRewrite.exe'
$out = Join-Path $repo 'meeting-e2e'
New-Item -ItemType Directory -Force -Path $out | Out-Null
if (-not (Test-Path -LiteralPath $exe)) { throw "Missing compiled Windows app: $exe" }

Add-Type -AssemblyName System.Drawing
Add-Type -AssemblyName System.Windows.Forms
Add-Type @'
using System;
using System.Runtime.InteropServices;
public static class MeetingE2EWin {
    [StructLayout(LayoutKind.Sequential)]
    public struct RECT { public int Left, Top, Right, Bottom; }
    [DllImport("user32.dll", CharSet=CharSet.Unicode)]
    public static extern IntPtr FindWindow(string className, string title);
    [DllImport("user32.dll")]
    public static extern IntPtr GetDlgItem(IntPtr hwnd, int id);
    [DllImport("user32.dll")]
    public static extern bool IsWindowVisible(IntPtr hwnd);
    [DllImport("user32.dll")]
    public static extern bool IsWindowEnabled(IntPtr hwnd);
    [DllImport("user32.dll")]
    public static extern bool GetWindowRect(IntPtr hwnd, out RECT rect);
    [DllImport("user32.dll")]
    public static extern IntPtr SendMessage(IntPtr hwnd, uint msg, IntPtr wp, IntPtr lp);
    [DllImport("user32.dll")]
    public static extern uint GetWindowThreadProcessId(IntPtr hwnd, out uint processId);
}
'@

$proc = Start-Process -FilePath $exe -ArgumentList '--meeting-demo' -PassThru
$report = @()
try {
    $hwnd = [IntPtr]::Zero
    for ($i=0; $i -lt 60; $i++) {
        Start-Sleep -Milliseconds 200
        $hwnd = [MeetingE2EWin]::FindWindow('VoiceRewrite.Meeting', 'VoiceRewrite · 会议助手')
        if ($hwnd -ne [IntPtr]::Zero) { break }
        $proc.Refresh()
        if ($proc.HasExited) { throw "Meeting demo exited with $($proc.ExitCode)" }
    }
    if ($hwnd -eq [IntPtr]::Zero) { throw 'Real Meeting Win32 window did not appear' }
    [uint32]$ownerPID = 0
    [void][MeetingE2EWin]::GetWindowThreadProcessId($hwnd, [ref]$ownerPID)
    if ($ownerPID -ne $proc.Id) { throw "Meeting HWND belongs to PID $ownerPID, not test PID $($proc.Id)" }

    $englishButton = [MeetingE2EWin]::GetDlgItem($hwnd, 7115)
    $suggestButton = [MeetingE2EWin]::GetDlgItem($hwnd, 7106)
    $captionsButton = [MeetingE2EWin]::GetDlgItem($hwnd, 7101)
    $copyButton = [MeetingE2EWin]::GetDlgItem($hwnd, 7105)
    foreach ($control in @($englishButton,$suggestButton,$captionsButton,$copyButton)) {
        if ($control -eq [IntPtr]::Zero) { throw 'Missing a real Meeting action button' }
    }

    $cases = @('expanded-english', 'full-workspace', 'captions-only', 'larger-text')
    foreach ($case in $cases) {
        switch ($case) {
            'expanded-english' {
                if (-not [MeetingE2EWin]::IsWindowVisible($englishButton) -or
                    [MeetingE2EWin]::IsWindowVisible($suggestButton) -or
                    -not [MeetingE2EWin]::IsWindowEnabled($copyButton)) {
                    throw 'Demo did not start with readable/copyable English and hidden suggestion tools'
                }
            }
            'full-workspace' {
                [void][MeetingE2EWin]::SendMessage($hwnd, 0x0111, [IntPtr]7115, [IntPtr]::Zero)
                if (-not [MeetingE2EWin]::IsWindowVisible($suggestButton)) {
                    throw 'Native collapse-English button did not reveal the suggestion controls'
                }
            }
            'captions-only' {
                [void][MeetingE2EWin]::SendMessage($hwnd, 0x0111, [IntPtr]7101, [IntPtr]::Zero)
                if ([MeetingE2EWin]::IsWindowVisible($englishButton) -or
                    [MeetingE2EWin]::IsWindowVisible($suggestButton)) {
                    throw 'Captions-only mode still shows assistant controls'
                }
            }
            'larger-text' {
                [void][MeetingE2EWin]::SendMessage($hwnd, 0x0111, [IntPtr]7101, [IntPtr]::Zero)
                [void][MeetingE2EWin]::SendMessage($hwnd, 0x0111, [IntPtr]7103, [IntPtr]::Zero)
                [void][MeetingE2EWin]::SendMessage($hwnd, 0x0111, [IntPtr]7103, [IntPtr]::Zero)
                if (-not [MeetingE2EWin]::IsWindowVisible($suggestButton)) {
                    throw 'Restoring full workspace after resizing text failed'
                }
            }
        }

        Start-Sleep -Milliseconds 250
        $bounds = [MeetingE2EWin+RECT]::new()
        if (-not [MeetingE2EWin]::GetWindowRect($hwnd, [ref]$bounds)) { throw "Missing Meeting window bounds in $case" }
        $w = $bounds.Right - $bounds.Left
        $h = $bounds.Bottom - $bounds.Top
        $desktop = [System.Windows.Forms.SystemInformation]::VirtualScreen
        if ($w -lt 560 -or $h -lt 640 -or
            $bounds.Left -lt $desktop.Left -or $bounds.Top -lt $desktop.Top -or
            $bounds.Right -gt $desktop.Right -or $bounds.Bottom -gt $desktop.Bottom) {
            throw "${case}: window is too small or clipped: $w x $h; virtual desktop $desktop"
        }
        $bitmap = [System.Drawing.Bitmap]::new($w,$h)
        $graphics = [System.Drawing.Graphics]::FromImage($bitmap)
        try {
            $graphics.CopyFromScreen($bounds.Left,$bounds.Top,0,0,[System.Drawing.Size]::new($w,$h))
            $bitmap.Save((Join-Path $out "$case.png"), [System.Drawing.Imaging.ImageFormat]::Png)
        }
        finally {
            $graphics.Dispose()
            $bitmap.Dispose()
        }
        $file = Join-Path $out "$case.png"
        if ((Get-Item -LiteralPath $file).Length -lt 2500) {
            throw "Meeting screenshot is suspiciously small: $case"
        }
        $report += [pscustomobject]@{
            case = $case
            processId = $proc.Id
            width = $w
            height = $h
            virtualDesktop = $desktop.ToString()
            englishButtonVisible = [MeetingE2EWin]::IsWindowVisible($englishButton)
            suggestionButtonVisible = [MeetingE2EWin]::IsWindowVisible($suggestButton)
            copyButtonEnabled = [MeetingE2EWin]::IsWindowEnabled($copyButton)
            screenshotBytes = (Get-Item $file).Length
        }
        Write-Host "PASS native Meeting screenshot: $case ($w x $h)"
    }
    $report | ConvertTo-Json -Depth 4 | Set-Content -LiteralPath (Join-Path $out 'diagnostics.json') -Encoding utf8
}
finally {
    $proc.Refresh()
    if (-not $proc.HasExited) { Stop-Process -Id $proc.Id -Force -ErrorAction SilentlyContinue }
}
