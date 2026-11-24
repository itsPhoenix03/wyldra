# Opens the dev stack's ports so other machines on your Tailscale network can
# play against this one, and closes them again. Windows only.
#
#   npm run start:dev:open-ports   open them for as long as the dev stack runs
#   npm run ports:open             open them until you close them
#   npm run ports:close            close them (also tidies up after a reboot)
#   npm run ports:status           say whether they are open
#
# Options go after `--`, e.g. `npm run ports:open -- -Interface Wi-Fi` for a LAN:
#   -Ports      comma-separated TCP ports     (default 5173,8090,8082)
#   -Interface  network adapter to accept on  (default Tailscale)
#
# Two things stand between a tailnet peer and this machine, and opening handles both:
#
#  1. Windows Firewall. Changing it needs admin, so a normal terminal gets one UAC
#     prompt. The rule accepts connections only on the named adapter, and only from
#     Tailscale's shared address block (100.64.0.0/10 and fd7a:115c:a1e0::/48, the
#     same for every tailnet) or, on any other adapter, from its local subnet. It
#     allows ports rather than programs because `go run` rebuilds the game server
#     to a new path after every change, which a per-program rule would stop matching.
#
#  2. Tailscale's own "shields up" (the client's "Allow incoming connections"
#     switch turned off), which refuses every incoming connection before the
#     firewall sees it. If it is up, opening lowers it, the rule remembers that,
#     and closing puts it back up. If it was already down it is left alone.
#
# -WatchPid ties the rule to a process: a hidden elevated helper opens the ports,
# waits for that process to exit, then closes them. tools/dev/stack.mjs passes its
# own pid, so the ports close however the stack ends: Ctrl-C, a crash or a closed
# terminal. Only a reboot can strand the rule; `npm run ports:close` removes it.

param(
  [Parameter(Position = 0)] [ValidateSet('open', 'close', 'status')] [string] $Action = 'status',
  [string] $Ports = '5173,8090,8082',  # text, not [int[]]: -File hands every argument over as a string
  [string] $Interface = 'Tailscale',
  [int] $WatchPid = 0,
  [switch] $ShieldsWereUp,             # internal: record in the rule that closing must raise them
  [switch] $Helper                     # internal: the elevated half of -WatchPid
)

$ErrorActionPreference = 'Stop'
$RuleName = 'Wyldra dev stack (remote play)'
$TailscaleRanges = @('100.64.0.0/10', 'fd7a:115c:a1e0::/48')
$ShieldsNote = 'Tailscale shields were up; closing puts them back.'

function Test-Admin {
  $id = [Security.Principal.WindowsIdentity]::GetCurrent()
  ([Security.Principal.WindowsPrincipal] $id).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
}

function Get-Rule { Get-NetFirewallRule -DisplayName $RuleName -ErrorAction SilentlyContinue }

function Get-PortList {
  $list = @($Ports -split '[,\s]+' | Where-Object { $_ } | ForEach-Object {
    $n = 0
    if (-not [int]::TryParse($_, [ref] $n) -or $n -lt 1 -or $n -gt 65535) { throw "Not a port: '$_'" }
    $n
  })
  if ($list.Count -eq 0) { throw 'No ports given.' }
  , $list
}

function Get-TailscaleExe {
  $cmd = Get-Command tailscale -ErrorAction SilentlyContinue
  if ($cmd) { return $cmd.Source }
  $default = Join-Path $env:ProgramFiles 'Tailscale\tailscale.exe'
  if (Test-Path $default) { return $default }
  $null
}

# $null when Tailscale is not installed or not answering.
function Get-ShieldsUp {
  $exe = Get-TailscaleExe
  if (-not $exe) { return $null }
  try { [bool] ((& $exe debug prefs 2>$null | Out-String) | ConvertFrom-Json).ShieldsUp } catch { $null }
}

function Set-Shields([bool] $up) {
  $exe = Get-TailscaleExe
  if (-not $exe) { throw 'tailscale.exe not found' }
  & $exe set "--shields-up=$($up.ToString().ToLower())"
  if ($LASTEXITCODE -ne 0) { throw "tailscale set --shields-up=$($up.ToString().ToLower()) failed" }
}

function Close-Ports {
  $rule = Get-Rule
  if (-not $rule) { return }
  $raise = $rule.Description -like "*$ShieldsNote*"
  $rule | Remove-NetFirewallRule
  if ($raise) { Set-Shields $true }
}

function Open-Ports([int[]] $list) {
  # Replacing a rule must not forget that an earlier open lowered the shields.
  $old = Get-Rule
  $note = ''
  if ($ShieldsWereUp -or ($old -and $old.Description -like "*$ShieldsNote*")) { $note = " $ShieldsNote" }
  if ($old) { $old | Remove-NetFirewallRule }
  if ($Interface -eq 'Tailscale') { $from = $TailscaleRanges } else { $from = 'LocalSubnet' }
  $null = New-NetFirewallRule -DisplayName $RuleName -Direction Inbound -Action Allow -Protocol TCP `
    -LocalPort $list -InterfaceAlias $Interface -RemoteAddress $from -Profile Any `
    -Description "TCP $($list -join ', ') on $Interface. Added by tools/dev/firewall.ps1; npm run ports:close removes it.$note"
}

# Runs this script again with admin rights: through one UAC prompt from a normal
# terminal, as a plain hidden child when already elevated. Throws if the prompt
# is declined. The window is hidden, so anything that can fail on bad input is
# checked before this is called.
function Start-Elevated([string[]] $extra) {
  $argv = @('-NoProfile', '-ExecutionPolicy', 'Bypass', '-File', "`"$PSCommandPath`"", $Action,
            '-Ports', "`"$Ports`"", '-Interface', "`"$Interface`"") + $extra
  $opts = @{ FilePath = 'powershell.exe'; ArgumentList = $argv; WindowStyle = 'Hidden'; PassThru = $true }
  if (-not (Test-Admin)) { $opts.Verb = 'RunAs' }
  Start-Process @opts
}

try {
  if ($Helper) {
    Open-Ports (Get-PortList)
    if ($WatchPid) { Wait-Process -Id $WatchPid -ErrorAction SilentlyContinue }
    Close-Ports
    exit 0
  }

  if ($Action -eq 'status') {
    $rule = Get-Rule
    if ($rule) { Write-Host "open: $($rule.Description)" } else { Write-Host 'closed' }
    $shields = Get-ShieldsUp
    if ($shields -eq $true) { Write-Host 'Tailscale shields: up (it refuses all incoming connections)' }
    elseif ($shields -eq $false) { Write-Host 'Tailscale shields: down (incoming connections allowed)' }
    exit 0
  }

  if ($Action -eq 'close') {
    $rule = Get-Rule
    $raise = $rule -and $rule.Description -like "*$ShieldsNote*"
    if (Test-Admin) { Close-Ports } else { (Start-Elevated @()).WaitForExit() }
    if (Get-Rule) { throw 'the firewall rule is still there' }
    # The elevated run raised the shields too; this covers it failing there unseen.
    if ($raise -and (Get-ShieldsUp) -eq $false) { Set-Shields $true }
    Write-Host 'closed'
    exit 0
  }

  $list = Get-PortList
  if (-not (Get-NetAdapter -Name $Interface -ErrorAction SilentlyContinue)) {
    throw "no network adapter named '$Interface'. Pass -Interface with one of: $((Get-NetAdapter).Name -join ', ')"
  }
  $lower = $Interface -eq 'Tailscale' -and (Get-ShieldsUp) -eq $true
  $extra = @()
  if ($lower) { $extra += '-ShieldsWereUp' }

  if ($WatchPid) {
    $proc = Start-Elevated ($extra + @('-WatchPid', "$WatchPid", '-Helper'))
    $deadline = (Get-Date).AddMinutes(2)
    while (-not (Get-Rule)) {
      if ($proc.WaitForExit(250) -or (Get-Date) -gt $deadline) { throw 'the elevated helper did not add the rule' }
    }
    $until = "closing when process $WatchPid exits"
  } else {
    if (Test-Admin) { Open-Ports $list } else { (Start-Elevated $extra).WaitForExit() }
    if (-not (Get-Rule)) { throw 'the firewall rule was not added' }
    $until = 'until npm run ports:close'
  }
  # Lowered only once the rule exists, so a declined UAC prompt changes nothing.
  if ($lower) {
    Set-Shields $false
    Write-Host 'Tailscale shields lowered (they were refusing all incoming connections); closing raises them again'
  }
  Write-Host "open: TCP $($list -join ', ') on $Interface, $until"
} catch {
  Write-Host "Could not $Action the ports: $($_.Exception.Message)"
  exit 1
}
