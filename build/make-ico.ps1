# make-ico.ps1 - regenerate build/icon.ico from the master icon.png (multi-size: 256 PNG + 64/48/32/16 BMP).
# Usage: pwsh -File build/make-ico.ps1 -Source icon.png -Output build/icon.ico
# Packs a square transparent PNG into a multi-size .ico.
# 256px entry is PNG-compressed; 64/48/32/16 are classic 32-bit BMP entries.
param(
    [Parameter(Mandatory=$true)][string]$Source,
    [Parameter(Mandatory=$true)][string]$Output
)
Add-Type -AssemblyName System.Drawing

$dir = Split-Path $Output -Parent
if ($dir -and -not (Test-Path $dir)) { New-Item -ItemType Directory -Path $dir | Out-Null }

$src = [System.Drawing.Bitmap]::new($Source)
if ($src.Width -ne $src.Height) { throw "source must be square" }

function New-Resized([System.Drawing.Bitmap]$bmp, [int]$n) {
    $out = [System.Drawing.Bitmap]::new($n, $n, [System.Drawing.Imaging.PixelFormat]::Format32bppArgb)
    $g = [System.Drawing.Graphics]::FromImage($out)
    $g.InterpolationMode = [System.Drawing.Drawing2D.InterpolationMode]::HighQualityBicubic
    $g.SmoothingMode = [System.Drawing.Drawing2D.SmoothingMode]::HighQuality
    $g.PixelOffsetMode = [System.Drawing.Drawing2D.PixelOffsetMode]::HighQuality
    $g.DrawImage($bmp, (New-Object System.Drawing.Rectangle(0, 0, $n, $n)))
    $g.Dispose()
    return $out
}

function Get-PngBytes([System.Drawing.Bitmap]$bmp) {
    $ms = [System.IO.MemoryStream]::new()
    $bmp.Save($ms, [System.Drawing.Imaging.ImageFormat]::Png)
    $b = $ms.ToArray(); $ms.Dispose(); return ,$b
}

function Get-BmpEntry([System.Drawing.Bitmap]$bmp) {
    $n = $bmp.Width
    $rect = New-Object System.Drawing.Rectangle(0, 0, $n, $n)
    $data = $bmp.LockBits($rect, [System.Drawing.Imaging.ImageLockMode]::ReadOnly, [System.Drawing.Imaging.PixelFormat]::Format32bppArgb)
    $stride = $data.Stride
    $px = New-Object byte[] ($n * 4 * $n)
    for ($row = 0; $row -lt $n; $row++) {
        $srcRow = ($n - 1 - $row) * $stride        # bottom-up rows
        for ($x = 0; $x -lt $n; $x++) {
            $o = $row * $n * 4 + $x * 4
            $px[$o]     = [System.Runtime.InteropServices.Marshal]::ReadByte($data.Scan0, $srcRow + $x * 4)
            $px[$o + 1] = [System.Runtime.InteropServices.Marshal]::ReadByte($data.Scan0, $srcRow + $x * 4 + 1)
            $px[$o + 2] = [System.Runtime.InteropServices.Marshal]::ReadByte($data.Scan0, $srcRow + $x * 4 + 2)
            $px[$o + 3] = [System.Runtime.InteropServices.Marshal]::ReadByte($data.Scan0, $srcRow + $x * 4 + 3)
        }
    }
    $bmp.UnlockBits($data)
    $andRow = [int]([math]::Ceiling([math]::Ceiling($n / 8.0) / 4.0) * 4)
    $and = New-Object byte[] ($andRow * $n)      # AND mask all zero = use alpha

    $ms = [System.IO.MemoryStream]::new()
    $bw = [System.IO.BinaryWriter]::new($ms)
    $bw.Write([uint32]40); $bw.Write([uint32]$n); $bw.Write([uint32]($n * 2))
    $bw.Write([uint16]1); $bw.Write([uint16]32); $bw.Write([uint32]0)
    $bw.Write([uint32]($px.Length + $and.Length)); $bw.Write([uint32]0); $bw.Write([uint32]0)
    $bw.Write([uint32]0); $bw.Write([uint32]0)
    $bw.Write($px); $bw.Write($and)
    $bw.Flush()
    $b = $ms.ToArray(); $bw.Dispose(); $ms.Dispose()
    return ,$b
}

$sizes = @(256, 64, 48, 32, 16)
$pngs = @{}
$bmps = @{}
foreach ($n in $sizes) {
    $b = New-Resized $src $n
    if ($n -eq 256) { $pngs[$n] = Get-PngBytes $b } else { $bmps[$n] = Get-BmpEntry $b }
    $b.Dispose()
}
$src.Dispose()

$ms = [System.IO.MemoryStream]::new()
$bw = [System.IO.BinaryWriter]::new($ms)
$bw.Write([uint16]0); $bw.Write([uint16]1); $bw.Write([uint16]$sizes.Count)
$offset = 6 + 16 * $sizes.Count
foreach ($n in $sizes) {
    $sz = if ($n -eq 256) { $pngs[$n].Length } else { $bmps[$n].Length }
    $byteDim = if ($n -ge 256) { 0 } else { $n }
    $bw.Write([byte]$byteDim); $bw.Write([byte]$byteDim)
    $bw.Write([byte]0); $bw.Write([byte]0)
    $bw.Write([uint16]1); $bw.Write([uint16]32)
    $bw.Write([uint32]$sz); $bw.Write([uint32]$offset)
    $offset += $sz
}
foreach ($n in $sizes) {
    if ($n -eq 256) { $bw.Write($pngs[$n]) } else { $bw.Write($bmps[$n]) }
}
$bw.Flush()
[System.IO.File]::WriteAllBytes($Output, $ms.ToArray())
$bw.Dispose(); $ms.Dispose()
Write-Output ("ico written: {0} ({1:N1} KB)" -f $Output, ((Get-Item $Output).Length / 1KB))

