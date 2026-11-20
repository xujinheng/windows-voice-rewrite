$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
$repo = Split-Path -Parent $PSScriptRoot
$exe = Join-Path $repo 'clients/windows/VoiceRewrite.exe'
$out = Join-Path $repo 'acceptance-output/windows'
New-Item -Force -ItemType Directory -Path $out | Out-Null
if (-not (Test-Path $exe)) { throw "Missing compiled executable $exe" }
Add-Type -AssemblyName System.Drawing
Add-Type -AssemblyName System.Windows.Forms
Add-Type @'
using System;
using System.Runtime.InteropServices;
public class AcceptanceWin {
  [StructLayout(LayoutKind.Sequential)] public struct RECT {public int Left,Top,Right,Bottom;}
  [DllImport("user32.dll",CharSet=CharSet.Unicode)] public static extern IntPtr FindWindow(string c,string t);
  [DllImport("user32.dll")] public static extern bool GetWindowRect(IntPtr h,out RECT r);
  [DllImport("user32.dll")] public static extern uint GetWindowThreadProcessId(IntPtr h,out uint pid);
  [DllImport("user32.dll")] public static extern IntPtr SendMessage(IntPtr h,uint m,IntPtr w,IntPtr l);
  [DllImport("user32.dll",CharSet=CharSet.Unicode)] public static extern IntPtr GetProp(IntPtr h,string key);
}
'@
$cases = @(
  @{state='settings.speech';kind='settings';theme='light';fixture='default-settings-v1';page=0},
  @{state='settings.writing';kind='settings';theme='light';fixture='default-settings-v1';page=1},
  @{state='settings.input';kind='settings';theme='light';fixture='default-settings-v1';page=2},
  @{state='settings.speech';kind='settings';theme='dark';fixture='default-settings-v1';page=0},
  @{state='settings.writing';kind='settings';theme='dark';fixture='default-settings-v1';page=1},
  @{state='settings.input';kind='settings';theme='dark';fixture='default-settings-v1';page=2},
  @{state='pill.preview';kind='recording';theme='dark';fixture='synthetic-dictation-v1';phase='preview'},
  @{state='pill.recording';kind='recording';theme='dark';fixture='synthetic-dictation-v1';phase='recording'},
  @{state='pill.processing';kind='recording';theme='dark';fixture='synthetic-dictation-v1';phase='processing'},
  @{state='pill.failure';kind='recording';theme='dark';fixture='synthetic-dictation-v1';phase='failure'},
  @{state='pill.success-event-sent';kind='recording';theme='dark';fixture='synthetic-dictation-v1';phase='success-event-sent'},
  @{state='result.copy';kind='answer';theme='light';fixture='result-v1'},
  @{state='result.copy';kind='answer';theme='dark';fixture='result-v1'},
  @{state='result.apply';kind='answer';theme='light';fixture='result-apply-v1';apply='1'},
  @{state='result.apply';kind='answer';theme='dark';fixture='result-apply-v1';apply='1'},
  @{state='result.long';kind='answer';theme='light';fixture='long-result-v1';long='1'},
  @{state='result.long';kind='answer';theme='dark';fixture='long-result-v1';long='1'}
)
$captures = @()
foreach ($case in $cases) {
  $env:VOICEREWRITE_VISUAL_PREVIEW = $case.kind
  $env:VOICEREWRITE_VISUAL_THEME = $case.theme
  $env:VOICEREWRITE_VISUAL_SCALE = '1'
  $env:VOICEREWRITE_VISUAL_STATE = if ($case.ContainsKey('phase')) { $case.phase } else { 'recording' }
  $env:VOICEREWRITE_VISUAL_APPLY = if ($case.ContainsKey('apply')) { $case.apply } else { '0' }
  $env:VOICEREWRITE_VISUAL_LONG = if ($case.ContainsKey('long')) { $case.long } else { '0' }
  $class = switch ($case.kind) { settings {'VoiceRewrite.Settings'} answer {'VoiceRewrite.ResultCard'} recording {'VoiceRewrite.RecordingIndicator'} }
  $title = switch ($case.kind) { settings {'VoiceRewrite · 连接设置'} answer {'VoiceRewrite'} recording {'VoiceRewrite status'} }
  $proc = Start-Process -FilePath $exe -PassThru
  try {
    $hwnd = [IntPtr]::Zero
    for ($i=0; $i -lt 70; $i++) {
      Start-Sleep -Milliseconds 100
      $hwnd = [AcceptanceWin]::FindWindow($class,$title)
      if ($hwnd -ne [IntPtr]::Zero) {break}
      $proc.Refresh()
      if ($proc.HasExited) {throw "Preview exited: $($case.state)"}
    }
    if ($hwnd -eq [IntPtr]::Zero) {throw "No actual window: $($case.state)"}
    [uint32]$pidOwner=0
    [void][AcceptanceWin]::GetWindowThreadProcessId($hwnd,[ref]$pidOwner)
    if ($pidOwner -ne $proc.Id) {throw "Window PID mismatch for $($case.state)"}
    if ($case.kind -eq 'settings') {
      for ($i=0;$i -lt 30 -and [AcceptanceWin]::GetProp($hwnd,'VoiceRewrite.Direct2D.FrameReady') -eq [IntPtr]::Zero;$i++) {
        Start-Sleep -Milliseconds 100
      }
      if ([AcceptanceWin]::GetProp($hwnd,'VoiceRewrite.Direct2D.FrameReady') -eq [IntPtr]::Zero) {throw 'Settings Direct2D renderer not ready'}
      $button = [int](9120+$case.page)
      [void][AcceptanceWin]::SendMessage($hwnd,0x0111,[IntPtr]$button,[IntPtr]::Zero)
    }
    Start-Sleep -Milliseconds 180
    $rect = [AcceptanceWin+RECT]::new()
    if (-not [AcceptanceWin]::GetWindowRect($hwnd,[ref]$rect)) {throw 'No window rect'}
    $width=$rect.Right-$rect.Left; $height=$rect.Bottom-$rect.Top
    $desktop=[System.Windows.Forms.SystemInformation]::VirtualScreen
    if ($width -lt 100 -or $height -lt 30 -or $rect.Left -lt $desktop.Left -or $rect.Top -lt $desktop.Top -or $rect.Right -gt $desktop.Right -or $rect.Bottom -gt $desktop.Bottom) {throw "Clipped or invalid: $($case.state)"}
    $bitmap=[System.Drawing.Bitmap]::new($width,$height)
    $graphics=[System.Drawing.Graphics]::FromImage($bitmap)
    try {
      $graphics.CopyFromScreen($rect.Left,$rect.Top,0,0,[System.Drawing.Size]::new($width,$height))
      $file=($case.state + '-' + $case.theme + '.png')
      $bitmap.Save((Join-Path $out $file),[System.Drawing.Imaging.ImageFormat]::Png)
    } finally {$graphics.Dispose();$bitmap.Dispose()}
    $captures += @{state=$case.state;fixture=$case.fixture;theme=$case.theme;locale='zh-CN';scale='logical-1x';image=$file}
  } finally {
    $proc.Refresh()
    if (-not $proc.HasExited) {Stop-Process -Id $proc.Id -Force -ErrorAction SilentlyContinue}
  }
}
@{schema=1;platform='windows';classification='simulated-ui';captures=$captures} |
    ConvertTo-Json -Depth 5 | Set-Content -Encoding utf8 (Join-Path $out 'manifest.json')
