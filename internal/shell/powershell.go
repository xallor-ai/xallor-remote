package shell

import (
	"encoding/base64"
	"encoding/binary"
	"unicode/utf16"
)

const encodingPrefix = "[Console]::OutputEncoding = [Text.UTF8Encoding]::new($false); $OutputEncoding = [Console]::OutputEncoding; "

// Use the target interpreter's parser, not text matching: named blocks can
// coexist with declarations, nested blocks, comments, here-strings and Unicode.
// Status remains inside the final execution block because WinPS resets $?
// after block invocation. Never put exit in clean or dynamicparam: doing so
// could override an explicit exit or skip parameter binding.
const powerShellStatusAdapter = `
$__xrErrors=$null;
$__xrAst=[System.Management.Automation.Language.Parser]::ParseInput($__xrText,[ref]$null,[ref]$__xrErrors);
if ($__xrErrors.Count -gt 0) { throw $__xrErrors[0] };
$__xrBlock=$__xrAst.EndBlock;
if ($null -eq $__xrBlock) { $__xrBlock=$__xrAst.ProcessBlock };
if ($null -eq $__xrBlock) { $__xrBlock=$__xrAst.BeginBlock };
$__xrStatus=[char]10+'if (-not $?) { exit 1 } else { exit 0 }';
if ($null -eq $__xrBlock) {
  $__xrEnd='end {'+$__xrStatus+'}'+[char]10;
  if ($null -ne $__xrAst.CleanBlock) { $__xrText=$__xrText.Insert($__xrAst.CleanBlock.Extent.StartOffset,$__xrEnd) }
  else { $__xrText += [char]10+$__xrEnd }
} elseif ($__xrBlock.Unnamed) { $__xrText += $__xrStatus }
else { $__xrText=$__xrText.Insert($__xrBlock.Extent.EndOffset-1,$__xrStatus) };
& ([scriptblock]::Create($__xrText))`

func encode(command string) string {
	words := utf16.Encode([]rune(command))
	b := make([]byte, 2*len(words))
	for i, w := range words {
		binary.LittleEndian.PutUint16(b[2*i:], w)
	}
	return base64.StdEncoding.EncodeToString(b)
}

func powerShellArguments(command string) []string {
	// -EncodedCommand forces redirected WinPS stderr to CLIXML. This fixed
	// ASCII launcher carries only base64 data and keeps stderr as ordinary text.
	launcher := encodingPrefix + "$__xrText=[Text.Encoding]::Unicode.GetString([Convert]::FromBase64String('" + encode(command) + "'));" + powerShellStatusAdapter
	return []string{"-NoLogo", "-NoProfile", "-NonInteractive", "-Command", launcher}
}
