$ErrorActionPreference = 'Stop'
$forbidden = @('RA2MD.exe', 'gamemd.exe', 'YURI.exe', 'rules.ini', 'maps')
foreach ($name in $forbidden) {
    if (Get-ChildItem -Recurse -Force -File | Where-Object { $_.Name -ieq $name }) {
        throw "Proprietary game asset found in adapter repository: $name"
    }
}
Write-Host 'No proprietary game assets found.'

