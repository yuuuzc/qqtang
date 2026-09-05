[CmdletBinding()]
param(
    [ValidateSet('local', 'lan-host', 'remote-host')] [string] $Mode = '',
    [string] $ServerIP = '',
    [string] $ClientServerIP = '',
    [switch] $GMRemote
)

$ErrorActionPreference = 'Stop'
$workspace = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$settingsPath = Join-Path $workspace 'configs\network.json'
$clientDirectory = Join-Path $workspace 'runtime\client-patched'

function Test-PrivateIPv4([string] $Address) {
    $parsed = $null
    if (-not [Net.IPAddress]::TryParse($Address, [ref] $parsed) -or $parsed.AddressFamily -ne [Net.Sockets.AddressFamily]::InterNetwork) { return $false }
    $bytes = $parsed.GetAddressBytes()
    return $bytes[0] -eq 10 -or ($bytes[0] -eq 172 -and $bytes[1] -ge 16 -and $bytes[1] -le 31) -or ($bytes[0] -eq 192 -and $bytes[1] -eq 168)
}

function Test-IPv4([string] $Address) {
    $parsed = $null
    return [Net.IPAddress]::TryParse($Address, [ref] $parsed) -and
        $parsed.AddressFamily -eq [Net.Sockets.AddressFamily]::InterNetwork -and
        $Address -ne '0.0.0.0'
}

function Test-DNSName([string] $Address) {
	$Address = $Address.Trim().TrimEnd('.')
	return $Address.Contains('.') -and [Uri]::CheckHostName($Address) -eq [UriHostNameType]::Dns
}

function Resolve-EndpointIPv4([string] $Address) {
	$parsed = $null
	if ([Net.IPAddress]::TryParse($Address, [ref] $parsed) -and $parsed.AddressFamily -eq [Net.Sockets.AddressFamily]::InterNetwork) {
		return $parsed.ToString()
	}
	try {
		$candidates = @([Net.Dns]::GetHostAddresses($Address) |
			Where-Object { $_.AddressFamily -eq [Net.Sockets.AddressFamily]::InterNetwork -and $_.ToString() -ne '0.0.0.0' } |
			Sort-Object { $bytes = $_.GetAddressBytes(); ([uint64]$bytes[0] -shl 24) -bor ([uint64]$bytes[1] -shl 16) -bor ([uint64]$bytes[2] -shl 8) -bor $bytes[3] })
	}
	catch {
		throw "域名 $Address 无法解析为 IPv4：$($_.Exception.Message)"
	}
	if ($candidates.Count -eq 0) { throw "域名 $Address 没有 IPv4 A 记录。" }
	return $candidates[0].ToString()
}

function Get-PrivateIPv4Candidates {
    try {
        return @(Get-NetIPAddress -AddressFamily IPv4 -ErrorAction Stop |
            Where-Object { Test-PrivateIPv4 $_.IPAddress } |
            Select-Object -ExpandProperty IPAddress -Unique |
            Sort-Object)
    }
    catch {
        return @(Get-WmiObject Win32_NetworkAdapterConfiguration -Filter 'IPEnabled=True' -ErrorAction SilentlyContinue |
            ForEach-Object { $_.IPAddress } |
            Where-Object { Test-PrivateIPv4 $_ } |
            Sort-Object -Unique)
    }
}

function Get-UpdatedEndpointBytes([string] $Path, [string[]] $Keys, [string] $Address) {
    if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) { throw "客户端连接配置缺失：$Path" }
    $encoding = [Text.Encoding]::GetEncoding(28591)
    $text = $encoding.GetString([IO.File]::ReadAllBytes($Path))
    foreach ($key in $Keys) {
        $pattern = '(?im)^' + [Regex]::Escape($key) + '=[^\r\n]*'
        if (-not [Regex]::IsMatch($text, $pattern)) { throw "客户端连接配置 $Path 中没有 $key 字段。" }
        $text = [Regex]::Replace($text, $pattern, $key + '=' + $Address)
    }
    return ,$encoding.GetBytes($text)
}

function Save-NetworkSettings([string] $SelectedMode, [string] $AdvertisedAddress, [string] $ClientAddress, [bool] $ExposeGM) {
    $AdvertisedAddress = $AdvertisedAddress.Trim()
    $ClientAddress = $ClientAddress.Trim()
    if ($SelectedMode -eq 'local') {
        if ($AdvertisedAddress -ne '127.0.0.1') { throw '本机服务模式的公布地址必须是 127.0.0.1。' }
    }
    elseif ($SelectedMode -eq 'lan-host' -and -not (Test-PrivateIPv4 $AdvertisedAddress)) {
        throw '局域网服务模式必须选择本机的 10.x、172.16-31.x 或 192.168.x 私有 IPv4 地址。'
    }
    elseif ($SelectedMode -eq 'remote-host' -and (-not (Test-DNSName $AdvertisedAddress)) -and (-not (Test-IPv4 $AdvertisedAddress) -or (Test-PrivateIPv4 $AdvertisedAddress) -or $AdvertisedAddress -eq '127.0.0.1')) {
		throw '公网服务模式必须填写客户端可以访问的公网 IPv4 地址或域名。'
    }
	if (-not (Test-IPv4 $ClientAddress) -and -not (Test-DNSName $ClientAddress)) { throw '客户端服务器地址必须是有效 IPv4 或域名。' }
    if ($ExposeGM -and $SelectedMode -notin @('lan-host', 'remote-host')) { throw '只有局域网或公网服务模式可以开放远程 GM。' }

	$resolvedClientAddress = Resolve-EndpointIPv4 $ClientAddress
	$endpointFiles = @(
        @{ Path = (Join-Path $clientDirectory 'DirCfg.ini'); Keys = @('DirIP') },
        @{ Path = (Join-Path $clientDirectory 'p2psvrInfo.ini'); Keys = @('tcpip', 'udpip', 'stunip') },
        @{ Path = (Join-Path $clientDirectory 'caserver.ini'); Keys = @('ip', 'ip2') },
        @{ Path = (Join-Path $clientDirectory 'webserver.ini'); Keys = @('ip', 'ip2') }
    )
    $updates = @()
    foreach ($file in $endpointFiles) {
		$updates += [pscustomobject]@{ Path = $file.Path; Bytes = (Get-UpdatedEndpointBytes $file.Path $file.Keys $resolvedClientAddress) }
    }

    New-Item -ItemType Directory -Force -Path (Split-Path $settingsPath) | Out-Null
    $settings = [ordered]@{
        schema_version = 3
        mode = $SelectedMode
        server_ip = $AdvertisedAddress
        client_server_ip = $ClientAddress
        gm_remote = $ExposeGM
    }
    [IO.File]::WriteAllText($settingsPath, (($settings | ConvertTo-Json) + [Environment]::NewLine), [Text.UTF8Encoding]::new($false))
    foreach ($update in $updates) { [IO.File]::WriteAllBytes($update.Path, $update.Bytes) }
    return [pscustomobject]$settings
}

if ($Mode) {
    if (-not $ServerIP) { $ServerIP = if ($Mode -eq 'local') { '127.0.0.1' } else { throw '服务端模式必须提供 -ServerIP。' } }
    if (-not $ClientServerIP) { $ClientServerIP = $ServerIP }
    $saved = Save-NetworkSettings $Mode $ServerIP $ClientServerIP $GMRemote
    Write-Host "联机设置已保存：服务端 $($saved.mode)/$($saved.server_ip)；客户端 $($saved.client_server_ip)"
    return
}

Add-Type -AssemblyName System.Windows.Forms
Add-Type -AssemblyName System.Drawing
[Windows.Forms.Application]::EnableVisualStyles()

$current = [pscustomobject]@{ schema_version = 3; mode = 'local'; server_ip = '127.0.0.1'; client_server_ip = '127.0.0.1'; gm_remote = $false }
if (Test-Path -LiteralPath $settingsPath -PathType Leaf) {
    try {
        $loaded = Get-Content -Raw -LiteralPath $settingsPath | ConvertFrom-Json
        if ([int]$loaded.schema_version -eq 3) { $current = $loaded }
    } catch { }
}
$candidates = @(Get-PrivateIPv4Candidates)
$form = [Windows.Forms.Form]::new()
$form.Text = 'QQ堂本地版 - 联机设置'
$form.StartPosition = 'CenterScreen'
$form.ClientSize = [Drawing.Size]::new(560, 425)
$form.FormBorderStyle = 'FixedDialog'
$form.MaximizeBox = $false
$form.Font = [Drawing.Font]::new('Microsoft YaHei UI', 10)

$title = [Windows.Forms.Label]::new(); $title.Text = '服务器与客户端地址'; $title.Font = [Drawing.Font]::new('Microsoft YaHei UI', 16, [Drawing.FontStyle]::Bold); $title.AutoSize = $true; $title.Location = [Drawing.Point]::new(28, 24); $form.Controls.Add($title)
$modeLabel = [Windows.Forms.Label]::new(); $modeLabel.Text = '服务端模式'; $modeLabel.AutoSize = $true; $modeLabel.Location = [Drawing.Point]::new(30, 84); $form.Controls.Add($modeLabel)
$modeBox = [Windows.Forms.ComboBox]::new(); $modeBox.DropDownStyle = 'DropDownList'; $modeBox.Location = [Drawing.Point]::new(145, 80); $modeBox.Size = [Drawing.Size]::new(375, 28); [void]$modeBox.Items.AddRange(@('仅本机', '局域网', '公网部署')); $form.Controls.Add($modeBox)
$serverLabel = [Windows.Forms.Label]::new(); $serverLabel.Text = '公布 IP / 域名'; $serverLabel.AutoSize = $true; $serverLabel.Location = [Drawing.Point]::new(30, 133); $form.Controls.Add($serverLabel)
$serverBox = [Windows.Forms.ComboBox]::new(); $serverBox.DropDownStyle = 'DropDown'; $serverBox.Location = [Drawing.Point]::new(145, 129); $serverBox.Size = [Drawing.Size]::new(375, 28); foreach ($candidate in $candidates) { [void]$serverBox.Items.Add($candidate) }; $form.Controls.Add($serverBox)
$clientLabel = [Windows.Forms.Label]::new(); $clientLabel.Text = '客户端 IP / 域名'; $clientLabel.AutoSize = $true; $clientLabel.Location = [Drawing.Point]::new(30, 182); $form.Controls.Add($clientLabel)
$clientBox = [Windows.Forms.TextBox]::new(); $clientBox.Location = [Drawing.Point]::new(145, 178); $clientBox.Size = [Drawing.Size]::new(375, 28); $clientBox.Text = [string]$current.client_server_ip; $form.Controls.Add($clientBox)
$gmRemoteBox = [Windows.Forms.CheckBox]::new(); $gmRemoteBox.Text = '允许其他电脑访问 GM（必须先设置 GM 密码）'; $gmRemoteBox.Location = [Drawing.Point]::new(30, 222); $gmRemoteBox.Size = [Drawing.Size]::new(490, 28); $gmRemoteBox.Checked = [bool]$current.gm_remote; $form.Controls.Add($gmRemoteBox)
$help = [Windows.Forms.Label]::new(); $help.Location = [Drawing.Point]::new(30, 262); $help.Size = [Drawing.Size]::new(490, 82); $help.ForeColor = [Drawing.Color]::FromArgb(45, 93, 126); $form.Controls.Add($help)
$save = [Windows.Forms.Button]::new(); $save.Text = '保存'; $save.Location = [Drawing.Point]::new(415, 365); $save.Size = [Drawing.Size]::new(105, 38); $form.Controls.Add($save)
$cancel = [Windows.Forms.Button]::new(); $cancel.Text = '取消'; $cancel.Location = [Drawing.Point]::new(295, 365); $cancel.Size = [Drawing.Size]::new(105, 38); $cancel.DialogResult = [Windows.Forms.DialogResult]::Cancel; $form.Controls.Add($cancel); $form.CancelButton = $cancel

$modeValues = @('local', 'lan-host', 'remote-host')
$selectedIndex = [Array]::IndexOf($modeValues, [string]$current.mode); if ($selectedIndex -lt 0) { $selectedIndex = 0 }
$modeBox.SelectedIndex = $selectedIndex
$serverBox.Text = [string]$current.server_ip
$updateForm = {
    $selectedMode = $modeValues[$modeBox.SelectedIndex]
    if ($selectedMode -eq 'local') {
        $serverBox.Text = '127.0.0.1'; $serverBox.Enabled = $false
        $help.Text = '服务端只监听 127.0.0.1，适合单机或本机多开。客户端地址可单独指向其他服务器。'
    }
    elseif ($selectedMode -eq 'lan-host') {
        $serverBox.Enabled = $true
        if (-not (Test-PrivateIPv4 $serverBox.Text) -and $candidates.Count -gt 0) { $serverBox.Text = $candidates[0] }
        $help.Text = '服务端监听 0.0.0.0，并向好友显示选中的局域网 IP。Windows 若询问入站访问，可在可信专用网络上允许。'
    }
    else {
        $serverBox.Enabled = $true
        if (Test-PrivateIPv4 $serverBox.Text -or $serverBox.Text -eq '127.0.0.1') { $serverBox.Text = '' }
		$help.Text = '服务端监听 0.0.0.0；这里填写公网 IP 或域名。域名会解析为 IPv4，还需自行放行端口。'
    }
    $gmRemoteBox.Enabled = $selectedMode -in @('lan-host', 'remote-host')
    if (-not $gmRemoteBox.Enabled) { $gmRemoteBox.Checked = $false }
}
$modeBox.Add_SelectedIndexChanged($updateForm)
& $updateForm
$save.Add_Click({
    try {
        $saved = Save-NetworkSettings $modeValues[$modeBox.SelectedIndex] $serverBox.Text $clientBox.Text $gmRemoteBox.Checked
        [Windows.Forms.MessageBox]::Show("设置已保存。`r`n服务端公布地址：$($saved.server_ip)`r`n客户端连接地址：$($saved.client_server_ip)", 'QQ堂联机设置', 'OK', 'Information') | Out-Null
        $form.DialogResult = [Windows.Forms.DialogResult]::OK
        $form.Close()
    }
    catch { [Windows.Forms.MessageBox]::Show($_.Exception.Message, '无法保存', 'OK', 'Error') | Out-Null }
})
[void]$form.ShowDialog()
