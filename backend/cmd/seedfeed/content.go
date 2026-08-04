// content.go 生成帖子文案。语料按主题/场景/口吻组合，配合索引保证每条内容不重复。
package main

import (
	"fmt"
	"math/rand"
	"strings"
)

var videoTopics = []string{
	"蟹堡王后厨的一天",
	"三分钟复刻招牌蟹黄堡",
	"比奇堡骑行 vlog",
	"水母田捕捞实录",
	"菠萝屋大改造",
	"深夜食堂之海草汤",
	"新手也能学会的煎堡手法",
	"海边露营过夜全记录",
	"打工人的通勤日常",
	"周末赶海挖到什么",
	"厨房设备开箱测评",
	"一个人的城市漫游",
	"雨天窗边的慢时光",
	"从零开始学冲浪",
	"街头小吃扫荡计划",
	"清晨五点的海鲜市场",
}

var videoHooks = []string{
	"全程无剪辑",
	"建议先收藏再看",
	"高能预警",
	"新手友好版",
	"这次真的成了",
	"耗时三小时的成果",
	"最后一秒有惊喜",
	"手把手教学",
	"沉浸式体验",
	"真实还原",
}

var imageTopics = []string{
	"今日份美食打卡",
	"随手拍的海边黄昏",
	"小屋收纳改造前后",
	"这家咖啡店值得反复来",
	"周末市集淘到的宝贝",
	"我的桌面好物清单",
	"一个人也要好好吃饭",
	"通勤路上的九宫格",
	"雨后的街道意外好看",
	"新买的锅具试用报告",
	"厨房里的色彩实验",
	"旧物改造小记",
	"阳台上的绿植近况",
	"today's outfit 记录",
	"深夜书桌与一杯热可可",
	"把生活过成想要的样子",
}

var imageHooks = []string{
	"图多预警",
	"细节都在图里",
	"附详细清单",
	"平价好物",
	"零失败做法",
	"随手一拍都好看",
	"氛围感拉满",
	"低成本高质感",
	"记录一下",
	"强烈安利",
}

var scenes = []string{
	"比奇堡老城区",
	"蟹堡王门口",
	"贝壳沙滩",
	"海底大峡谷",
	"珊瑚集市",
	"菠萝屋二楼",
	"码头旧仓库",
	"街角那家小店",
	"城南的老巷子",
	"临海的公路边",
}

var openings = []string{
	"最近总有人问我这个怎么做，干脆一次性讲清楚。",
	"拖了很久终于拍完了，希望对你有帮助。",
	"今天状态不错，顺手记录一下。",
	"翻相册的时候发现这组素材，不发出来可惜了。",
	"评论区点名要的内容，安排上了。",
	"没有滤镜也没有摆拍，就是很普通的一天。",
	"第一次尝试这种风格，欢迎提意见。",
	"这条内容准备了挺久，细节都在里面了。",
}

var details = []string{
	"步骤不难，关键是火候和耐心，慢一点反而更稳。",
	"预算控制在一百块以内，性价比意外地高。",
	"中途翻车过两次，第三次才达到想要的效果。",
	"设备只有一台手机，能拍成这样我已经很满意。",
	"人不多，环境安静，很适合发呆放空。",
	"重点是配比，多一分少一分口感差别很明显。",
	"整理了一份清单放在最后，需要的自取。",
	"看似麻烦，实际上熟练之后十分钟就能搞定。",
}

var endings = []string{
	"下次想看什么，评论区告诉我。",
	"有问题随时问，看到都会回。",
	"如果觉得有用，记得点个收藏。",
	"更新不定期，但每条都认真做。",
	"欢迎交流经验，互相学习。",
}

var tagPool = []string{
	"#日常记录", "#美食", "#vlog", "#手作", "#生活方式", "#探店",
	"#海边", "#treasure", "#慢生活", "#好物分享", "#旅行", "#摄影",
	"#教程", "#开箱", "#氛围感", "#周末去哪儿",
}

// videoContent 生成视频帖的标题与正文。
func videoContent(i int, rnd *rand.Rand) (string, string) {
	title := fmt.Sprintf("Vol.%03d %s｜%s",
		i, pick(videoTopics, rnd), pick(videoHooks, rnd))
	desc := strings.Join([]string{
		pick(openings, rnd),
		"拍摄地点：" + pick(scenes, rnd) + "。" + pick(details, rnd),
		pick(endings, rnd),
		tagLine(rnd, 3),
	}, "\n")
	return title, desc
}

// imageContent 生成图文帖的标题与正文。
func imageContent(i int, rnd *rand.Rand) (string, string) {
	title := fmt.Sprintf("No.%03d %s｜%s",
		i, pick(imageTopics, rnd), pick(imageHooks, rnd))
	desc := strings.Join([]string{
		pick(openings, rnd),
		"地点在" + pick(scenes, rnd) + "，" + pick(details, rnd),
		pick(endings, rnd),
		tagLine(rnd, 4),
	}, "\n")
	return title, desc
}

func pick(list []string, rnd *rand.Rand) string {
	return list[rnd.Intn(len(list))]
}

// tagLine 随机挑选 n 个不重复话题标签。
func tagLine(rnd *rand.Rand, n int) string {
	if n > len(tagPool) {
		n = len(tagPool)
	}
	idx := rnd.Perm(len(tagPool))[:n]
	tags := make([]string, 0, n)
	for _, i := range idx {
		tags = append(tags, tagPool[i])
	}
	return strings.Join(tags, " ")
}
