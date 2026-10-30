$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
[Console]::OutputEncoding = [Text.UTF8Encoding]::new($false)
$capabilityMissing = $false
try {
    Add-Type -AssemblyName System.Runtime.WindowsRuntime
    $null = [Windows.Storage.Streams.InMemoryRandomAccessStream, Windows.Storage.Streams, ContentType=WindowsRuntime]
    $null = [Windows.Storage.Streams.DataWriter, Windows.Storage.Streams, ContentType=WindowsRuntime]
    $null = [Windows.Graphics.Imaging.BitmapDecoder, Windows.Graphics.Imaging, ContentType=WindowsRuntime]
    $null = [Windows.Graphics.Imaging.SoftwareBitmap, Windows.Graphics.Imaging, ContentType=WindowsRuntime]
    $null = [Windows.Media.Ocr.OcrEngine, Windows.Foundation, ContentType=WindowsRuntime]
    $null = [Windows.Media.Ocr.OcrResult, Windows.Foundation, ContentType=WindowsRuntime]
    $awaitMethod = [System.WindowsRuntimeSystemExtensions].GetMethods() | Where-Object { $_.Name -eq 'AsTask' -and $_.IsGenericMethod -and $_.GetParameters().Count -eq 1 -and $_.GetGenericArguments().Count -eq 1 } | Select-Object -First 1
    function Await-WinRT($Operation, $ResultType) {
        $task = $awaitMethod.MakeGenericMethod($ResultType).Invoke($null, @($Operation))
        $task.GetAwaiter().GetResult()
    }
    $languages = @([Windows.Media.Ocr.OcrEngine]::AvailableRecognizerLanguages)
    $language = $languages | Where-Object { $_.LanguageTag -like 'zh-Hans*' } | Select-Object -First 1
    if ($null -eq $language) { $language = $languages | Where-Object { $_.LanguageTag -like 'zh*' } | Select-Object -First 1 }
    if ($null -eq $language) { $language = $languages | Select-Object -First 1 }
    if ($null -eq $language) { $capabilityMissing = $true; throw 'Windows没有可用的OCR识别语言，未安装或修改任何系统组件。' }
    $engine = [Windows.Media.Ocr.OcrEngine]::TryCreateFromLanguage($language)
    if ($null -eq $engine) { $capabilityMissing = $true; throw 'Windows OCR引擎不可用。' }
} catch {
    [Console]::WriteLine((@{error=$_.Exception.Message;unavailable=$capabilityMissing} | ConvertTo-Json -Compress))
    exit 0
}
[Console]::WriteLine('{"ready":true}')
while ($null -ne ($line = [Console]::ReadLine())) {
    $stream = $null; $writer = $null; $bitmap = $null
    try {
        $request = $line | ConvertFrom-Json
        $bytes = [Convert]::FromBase64String([string]$request.png)
        if ($bytes.Length -gt 8388608) { throw 'Screenshot exceeds 8MiB' }
        $stream = [Windows.Storage.Streams.InMemoryRandomAccessStream]::new()
        $writer = [Windows.Storage.Streams.DataWriter]::new($stream)
        $writer.WriteBytes($bytes)
        $null = Await-WinRT $writer.StoreAsync() ([uint32])
        $null = $writer.DetachStream()
        $writer.Dispose(); $writer = $null
        $stream.Seek(0)
        $decoder = Await-WinRT ([Windows.Graphics.Imaging.BitmapDecoder]::CreateAsync($stream)) ([Windows.Graphics.Imaging.BitmapDecoder])
        $bitmap = Await-WinRT $decoder.GetSoftwareBitmapAsync() ([Windows.Graphics.Imaging.SoftwareBitmap])
        $result = Await-WinRT $engine.RecognizeAsync($bitmap) ([Windows.Media.Ocr.OcrResult])
        $lines = @()
        foreach ($ocrLine in $result.Lines) {
            $words = @()
            foreach ($word in $ocrLine.Words) {
                $r = $word.BoundingRect
                $words += @{text=$word.Text;bounds=@{x=[int][Math]::Floor($r.X);y=[int][Math]::Floor($r.Y);w=[int][Math]::Ceiling($r.Width);h=[int][Math]::Ceiling($r.Height)}}
            }
            $lines += @{words=$words}
        }
        [Console]::WriteLine((@{screen=@{language=$language.LanguageTag;lines=$lines}} | ConvertTo-Json -Depth 10 -Compress))
    } catch {
        [Console]::WriteLine((@{error=$_.Exception.Message;unavailable=$false} | ConvertTo-Json -Compress))
    } finally {
        if ($null -ne $bitmap) { $bitmap.Dispose() }
        if ($null -ne $writer) { $writer.Dispose() }
        if ($null -ne $stream) { $stream.Dispose() }
    }
}
