package keywordmatch

import (
	"sync"
	"testing"
)

// TestCompileExpressionCaseInsensitiveAcrossAlternation 验证 (?i) 覆盖整条表达式的所有分支。
func TestCompileExpressionCaseInsensitiveAcrossAlternation(t *testing.T) {
	// re、err 是顶层竖线表达式的编译结果。
	re, err := CompileExpression(" foo|bar ")
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	// text 是应被不同大小写命中的待匹配文本。
	for _, text := range []string{"FOO", "BAR", "xBarx"} {
		if !re.MatchString(text) {
			t.Fatalf("%q 应当命中", text)
		}
	}
}

// TestCompileExpressionRejectsWrapperEscape 验证不配对括号不能逃出忽略大小写范围。
func TestCompileExpressionRejectsWrapperEscape(t *testing.T) {
	// _, err 保存不配对括号表达式的编译结果，期望返回错误。
	if _, err := CompileExpression("foo)|(BAR"); err == nil {
		t.Fatal("不配对括号必须编译失败")
	}
}

// TestCompileExpressionInlineFlagOverride 验证用户仍可用 (?-i) 恢复大小写敏感。
func TestCompileExpressionInlineFlagOverride(t *testing.T) {
	// re 是显式大小写敏感的表达式。
	re, err := CompileExpression("(?-i)Abc")
	if err != nil {
		t.Fatal(err)
	}
	if re.MatchString("abc") || !re.MatchString("Abc") {
		t.Fatal("(?-i) 应恢复大小写敏感")
	}
}

// TestCompileExpressionCacheReuseAndReset 验证重复编译复用缓存，超限后清空仍可工作。
func TestCompileExpressionCacheReuseAndReset(t *testing.T) {
	// first、second 是同一表达式的两次编译结果，应为同一实例。
	first, _ := CompileExpression("cache-me")
	// second 是第二次编译的结果，用于与 first 比较实例是否相同。
	second, _ := CompileExpression("cache-me")
	if first != second {
		t.Fatal("同一表达式应复用缓存")
	}
	// i 是用于生成互不相同表达式的序号；err 是对应编译错误。
	for i := 0; i < maxCachedPatterns+5; i++ {
		// err 保存当前互不相同表达式的编译错误，期望为空。
		if _, err := CompileExpression(string(rune('a'+i%26)) + "x" + string(rune(0x4e00+i))); err != nil {
			t.Fatal(err)
		}
	}
	compiledCacheMu.RLock()
	// size 是超限清空后的缓存条目数。
	size := len(compiledCache)
	compiledCacheMu.RUnlock()
	if size > maxCachedPatterns {
		t.Fatalf("缓存条目 %d 超过上限", size)
	}
}

// TestCompileExpressionInvalid 验证语法错误不被缓存且返回错误。
func TestCompileExpressionInvalid(t *testing.T) {
	// _, err 保存语法错误表达式的编译结果，期望返回错误。
	if _, err := CompileExpression("("); err == nil {
		t.Fatal("非法表达式必须返回错误")
	}
}

// TestCompileExpressionTrimsAndSharesCacheKey 验证首尾空白不同的同一表达式共用同一缓存实例。
func TestCompileExpressionTrimsAndSharesCacheKey(t *testing.T) {
	// plain 是无空白表达式的编译结果。
	plain, _ := CompileExpression("trim-me")
	// padded 是带首尾空白的同一表达式的编译结果。
	padded, _ := CompileExpression("  trim-me\t\n")
	if plain != padded {
		t.Fatal("去除首尾空白后应复用同一缓存实例")
	}
}

// TestCompileExpressionEmpty 验证空表达式可编译并匹配任意文本，由调用方负责跳过空表达式。
func TestCompileExpressionEmpty(t *testing.T) {
	// re、err 是空表达式的编译结果。
	re, err := CompileExpression("   ")
	if err != nil || !re.MatchString("任意") {
		t.Fatalf("空表达式应可编译: %v", err)
	}
}

// TestCompileExpressionConcurrent 验证缓存在并发读写（含超限清空）下无数据竞争。
func TestCompileExpressionConcurrent(t *testing.T) {
	// wg 等待所有并发编译协程结束。
	var wg sync.WaitGroup
	// worker 是并发协程序号，用于生成部分重叠的表达式。
	for worker := 0; worker < 8; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			// n 是当前协程的迭代序号。
			for n := 0; n < 400; n++ {
				// err 保存本次并发编译的错误，期望为空。
				if _, err := CompileExpression(string(rune(0x4e00 + (n*7+worker)%1500))); err != nil {
					t.Error(err)
					return
				}
			}
		}(worker)
	}
	wg.Wait()
}
