// 「这一段**像不像**艺人名」的那一份**形状**判据（不是词典）。
//
// # 为什么要有它
//
// 词典（`artists.go`）是**一张名单**，而名单**不可能收全**——B 站上每天都有新
// 的歌手，而漏掉一个的后果是**那一首歌名认成歌手名**：界面上直接把歌手的名字
// 当歌名显示（"起风了 - 吴青峰"显示成「吴青峰」）。
//
// ⇒ 所以「哪一段是署名」这一档要有一份**不依赖名单**的形状判据：它认出「这一
// 段长得像一个人名 / 一个团名」。
//
// # ⚠️ **它只是「像」，不是「是」**
//
// ⇒ 所以它**不能用来选歌名**——它只用来**否决**：
//
//	后一段像艺人名 ⇒ 那条「艺人 - 歌名」的通行约定**不可信** ⇒ 认不出
//	              ⇒ 显示层退回**原标题**（宁可脏，不说谎）
//
// ⚠️ **误判的代价是「显示得更脏」，不是「显示错了」**：把一个歌名当成艺人名，
// 后果是那一条标题退回原样（"X - 白月光"显示成「X - 白月光」）；而反过来（把
// 艺人名当成歌名）才是**界面说谎**——所以这份判据**宁可多命中**。
//
// # 为什么不给它一份「常见歌名」表
//
// 那是一份**收不完**的名单（而这份名单收不完的后果就是今天这个 bug）。
// ⇒ 所以这里只看**形状**：姓氏、字数、大小写。
package songmeta

import "strings"

// looksLikeArtistName 报这一段**像不像**一个艺人名（人名 / 团名）。
//
// other 是**另一段**的内容（破折号 / 竖线那一档的另一半）——它只用来判「中英
// 混排」那一档：一个**拉丁字母**的名字旁边是一段**汉字** ⇒ 那是团名
// （"海阔天空 - Beyond"）；而两段都是拉丁字母时按通行约定走（"Adele - Hello"
// ⇒ 歌名是「Hello」）。
//
// ⚠️ **汉字那一档不看 other**：中文人名和中文歌名长得一样（"吴青峰" / "白月
// 光"），只能靠姓氏那一条。
func looksLikeArtistName(s, other string) bool {
	t := strings.TrimSpace(s)
	// ⚠️ **两头的标点先去掉**（"吴青峰、"）：标题里那一段常常带着标点。
	t = strings.Trim(t, " ,，、|·・:：.。!！?？\"'“”‘’()（）[]【】《》「」『』")
	if t == "" {
		return false
	}
	if hasHan(t) {
		return looksLikeHanName(t)
	}
	return looksLikeLatinName(t, other)
}

// looksLikeHanName 报这一段**像不像**一个人名（姓 + 名）。
//
// # 判据是三样
//
//	① 整段都是汉字（没有别的字）；
//	② 两个字到四个字（姓 + 一到两个字的名；更长的多半是团名 / 组合名）；
//	③ **第一个字是常见姓氏**。
//
// ⚠️ **③ 是这一份判据的骨干**：中文歌名很少拿姓氏开头（"起风了"、"晴天"、"稻
// 香"、"海阔天空"、"体面"、"消愁"），而中文人名几乎都拿姓氏开头。
//
// ⚠️ **它会误判**：拿姓氏开头的歌名（"白月光"、"王妃"、"黄昏"）也会被当成人
// 名——而那份误判的代价见文件头（退回原标题，**不说谎**）。
func looksLikeHanName(t string) bool {
	r := []rune(t)
	if len(r) < 2 || len(r) > 4 {
		return false
	}
	for _, c := range r {
		if !isHanRune(c) {
			return false
		}
	}
	return commonSurnames[r[0]]
}

// looksLikeLatinName 报这一段**像不像**一个拉丁字母的团名 / 人名。
//
// # 判据是四样
//
//	① 整段是**拉丁字母**（没有别的字）；
//	② 一到三个词，**每个词的首字母是大写**（"Beyond"、"Linkin Park"）；小写
//	   开头的（"beyond"）不是「名字」的写法；
//	③ 每个词至少两个字母、整段至少三个字母；
//	④ **旁边那一段是汉字**（`other`）——见下面那一段。
//
// # ⚠️ **④ 为什么必须有**
//
// 纯英文的标题里**歌名和艺人名长得一样**（"Adele - Hello"），而 B 站上**通行
// 约定就是「艺人 - 歌名」**——所以纯英文那一档**按约定走**（歌名在后），不拿
// 这份判据去否决它。
//
// ⇒ 而「一段汉字 + 一段拉丁字母」是**中英混排**：那种写法里拉丁字母那一段几
// 乎总是团名（"海阔天空 - Beyond"）。
func looksLikeLatinName(t, other string) bool {
	// ⚠️ **④ 先判**：中英混排才轮到这一份判据（理由见上面那段）。
	if !hasHan(other) {
		return false
	}
	words := strings.Fields(t)
	if len(words) == 0 || len(words) > 3 {
		return false
	}
	total := 0
	for _, w := range words {
		r := []rune(w)
		if len(r) < 2 {
			return false
		}
		for i, c := range r {
			switch {
			case c >= 'A' && c <= 'Z':
			case c >= 'a' && c <= 'z':
				// ⚠️ **② 第二个词往后，首字母也要大写**（"Linkin Park"）。
				if i == 0 {
					return false
				}
			default:
				// ⚠️ **① 别的字（数字、连字符、点）不算**：那一段不是「名字」。
				return false
			}
		}
		total += len(r)
	}
	return total >= 3
}

// commonSurnames 是**常见姓氏**那一份名单（判据③用）。
//
// ⚠️ **它不是艺人名单**（那是 `artists.go` 那一份）——它是「**哪些字当头像是
// 一个人名**」的那一份。
//
// ⚠️ **收的是常见姓**（百家姓里最常见的那一批）：生僻姓（"海"、"赫"…）不进
// ——而那些姓开头的**歌名**（"海阔天空"）不该被当成人名。
//
// ⚠️ **宁可多收**：多收的代价是「某个歌名被当成艺人名」⇒ 那一条标题退回原
// 样（**不说谎**）；少收的代价是「某个艺人名被当成歌名」⇒ **界面说谎**。
var commonSurnames = map[rune]bool{
	'王': true, '李': true, '张': true, '刘': true, '陈': true, '杨': true,
	'黄': true, '赵': true, '吴': true, '周': true, '徐': true, '孙': true,
	'马': true, '朱': true, '胡': true, '郭': true, '何': true, '高': true,
	'林': true, '罗': true, '郑': true, '梁': true, '谢': true, '宋': true,
	'唐': true, '许': true, '韩': true, '冯': true, '邓': true, '曹': true,
	'彭': true, '曾': true, '肖': true, '田': true, '董': true, '袁': true,
	'潘': true, '于': true, '蒋': true, '蔡': true, '余': true, '杜': true,
	'叶': true, '程': true, '苏': true, '魏': true, '吕': true, '丁': true,
	'任': true, '沈': true, '姚': true, '卢': true, '姜': true, '崔': true,
	'钟': true, '谭': true, '陆': true, '汪': true, '范': true, '金': true,
	'石': true, '廖': true, '贾': true, '夏': true, '韦': true, '付': true,
	'方': true, '白': true, '邹': true, '孟': true, '熊': true, '秦': true,
	'邱': true, '江': true, '尹': true, '薛': true, '段': true, '雷': true,
	'侯': true, '龙': true, '史': true, '陶': true, '黎': true, '贺': true,
	'顾': true, '毛': true, '郝': true, '龚': true, '邵': true, '万': true,
	'钱': true, '严': true, '武': true, '戴': true, '莫': true, '孔': true,
	'向': true, '汤': true, '温': true, '柏': true, '窦': true, '章': true,
	'云': true, '葛': true, '奚': true, '郎': true, '鲁': true, '苗': true,
}
