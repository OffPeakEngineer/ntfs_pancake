param (
    [string]$DirectoryPath = $(Read-Host "Enter the directory path to scan"),
    [string]$OutputFilePath = $(Read-Host "Enter the output checksum file path")
)

# Function to compute the checksum of a file
function Get-FileChecksum {
    param (
        [string]$FilePath
    )

    # Create a hash algorithm instance
    $sha256 = [System.Security.Cryptography.SHA256]::Create()

    # Open the file for reading
    $stream = [System.IO.File]::OpenRead($FilePath)
    try {
        # Compute the hash
        $hashBytes = $sha256.ComputeHash($stream)
    }
    finally {
        # Ensure the file is closed
        $stream.Close()
    }

    # Convert the hash bytes to a hexadecimal string
    $hashString = [BitConverter]::ToString($hashBytes) -replace '-', ''
    return $hashString
}

# Ensure the output file is empty
Remove-Item -Path $OutputFilePath -ErrorAction Ignore
New-Item -Path $OutputFilePath -ItemType File

# Get a list of all files in the directory and subdirectories
$files = Get-ChildItem -Path $DirectoryPath -Recurse -File

# Compute checksums and write to the output file
foreach ($file in $files) {
    $checksum = Get-FileChecksum -FilePath $file.FullName
    "$($file.FullName) $checksum" | Out-File -FilePath $OutputFilePath -Append
}

Write-Host "Checksum file generated at $OutputFilePath"
