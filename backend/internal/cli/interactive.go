package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// readInput 读取用户输入
func readInput(prompt string, defaultValue string) string {
	reader := bufio.NewReader(os.Stdin)
	if defaultValue != "" {
		fmt.Printf("%s [%s]: ", prompt, defaultValue)
	} else {
		fmt.Printf("%s: ", prompt)
	}
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)
	if input == "" && defaultValue != "" {
		return defaultValue
	}
	return input
}

// manageSopKnowledges 交互式管理 SOP Knowledges
func manageSopKnowledges() []map[string]interface{} {
	return manageSopKnowledgesImpl([]map[string]interface{}{})
}

// manageSopKnowledgesWithInitial 带初始值的交互式管理 SOP Knowledges
func manageSopKnowledgesWithInitial(initialList []map[string]interface{}) []map[string]interface{} {
	return manageSopKnowledgesImpl(initialList)
}

// manageSopKnowledgesImpl 实际的 SOP Knowledges 管理实现
func manageSopKnowledgesImpl(sopList []map[string]interface{}) []map[string]interface{} {
	reader := bufio.NewReader(os.Stdin)

	fmt.Println("\n--- SOP Knowledge Configuration ---")
	fmt.Println("Commands: list, add, delete <num>, done")
	fmt.Println("Type 'help' for command details")

	for {
		fmt.Print("\nCommand (list/add/delete/done): ")
		cmd, _ := reader.ReadString('\n')
		cmd = strings.TrimSpace(strings.ToLower(cmd))

		parts := strings.Fields(cmd)
		if len(parts) == 0 {
			continue
		}

		mainCmd := parts[0]

		switch mainCmd {
		case "help", "h", "?":
			fmt.Println("\nAvailable commands:")
			fmt.Println("  list           - List all SOP knowledges")
			fmt.Println("  add            - Add a new SOP knowledge")
			fmt.Println("  delete <num>   - Delete SOP knowledge by number")
			fmt.Println("  done           - Finish configuration")

		case "list", "l", "ls":
			listSopKnowledges(sopList)

		case "add", "a":
			sopList = addSopKnowledge(sopList, reader)

		case "delete", "del", "d", "rm":
			if len(parts) < 2 {
				fmt.Println("Usage: delete <number>")
				continue
			}
			num, err := strconv.Atoi(parts[1])
			if err != nil || num < 1 || num > len(sopList) {
				fmt.Printf("Invalid number: %s\n", parts[1])
				continue
			}
			sopList = deleteSopKnowledge(sopList, num-1)

		case "done", "exit", "quit", "q":
			return sopList

		default:
			fmt.Printf("Unknown command: %s (type 'help' for available commands)\n", mainCmd)
		}
	}
}

// listSopKnowledges 列出所有 SOP Knowledges
func listSopKnowledges(sopList []map[string]interface{}) {
	if len(sopList) == 0 {
		fmt.Println("No SOP knowledges configured.")
		return
	}

	fmt.Printf("\n%d SOP Knowledge(s):\n", len(sopList))
	for i, sop := range sopList {
		fmt.Printf("[%d] ", i+1)
		sopType := ""
		if t, ok := sop["type"].(string); ok {
			fmt.Printf("Type: %s", t)
			sopType = t
		}

		// 根据类型显示不同字段
		switch sopType {
		case "oss":
			if r, ok := sop["region"].(string); ok {
				fmt.Printf(", Region: %s", r)
			}
			if b, ok := sop["bucket"].(string); ok {
				fmt.Printf(", Bucket: %s", b)
			}
			if p, ok := sop["basePath"].(string); ok {
				fmt.Printf(", BasePath: %s", p)
			}
			if d, ok := sop["description"].(string); ok && d != "" {
				fmt.Printf(", Desc: %s", d)
			}
		case "yunxiao":
			if o, ok := sop["organizationId"].(string); ok {
				fmt.Printf(", OrgID: %s", o)
			}
			if r, ok := sop["repositoryId"].(string); ok {
				fmt.Printf(", RepoID: %s", r)
			}
			if b, ok := sop["branchName"].(string); ok {
				fmt.Printf(", Branch: %s", b)
			}
			if p, ok := sop["basePath"].(string); ok {
				fmt.Printf(", BasePath: %s", p)
			}
			if d, ok := sop["description"].(string); ok && d != "" {
				fmt.Printf(", Desc: %s", d)
			}
		case "builtin":
			if id, ok := sop["id"].(string); ok {
				fmt.Printf(", ID: %s", id)
			}
		}
		fmt.Println()
	}
}

// addSopKnowledge 添加一个 SOP Knowledge
func addSopKnowledge(sopList []map[string]interface{}, reader *bufio.Reader) []map[string]interface{} {
	fmt.Println("\n--- Add SOP Knowledge ---")
	fmt.Print("Type (oss/yunxiao/builtin): ")
	sopType, _ := reader.ReadString('\n')
	sopType = strings.TrimSpace(sopType)

	sop := map[string]interface{}{
		"type": sopType,
	}

	switch sopType {
	case "oss":
		fmt.Print("Region: ")
		region, _ := reader.ReadString('\n')
		region = strings.TrimSpace(region)
		if region != "" {
			sop["region"] = region
		}

		fmt.Print("Bucket (required): ")
		bucket, _ := reader.ReadString('\n')
		bucket = strings.TrimSpace(bucket)
		if bucket == "" {
			fmt.Println("Error: Bucket is required for OSS type")
			return sopList
		}
		sop["bucket"] = bucket

		fmt.Print("Base Path (required): ")
		basePath, _ := reader.ReadString('\n')
		basePath = strings.TrimSpace(basePath)
		if basePath == "" {
			fmt.Println("Error: Base Path is required for OSS type")
			return sopList
		}
		sop["basePath"] = basePath

		fmt.Print("Description (optional): ")
		description, _ := reader.ReadString('\n')
		description = strings.TrimSpace(description)
		if description != "" {
			sop["description"] = description
		}

	case "yunxiao":
		fmt.Print("Organization ID (required): ")
		orgId, _ := reader.ReadString('\n')
		orgId = strings.TrimSpace(orgId)
		if orgId == "" {
			fmt.Println("Error: Organization ID is required for yunxiao type")
			return sopList
		}
		sop["organizationId"] = orgId

		fmt.Print("Repository ID (required): ")
		repoId, _ := reader.ReadString('\n')
		repoId = strings.TrimSpace(repoId)
		if repoId == "" {
			fmt.Println("Error: Repository ID is required for yunxiao type")
			return sopList
		}
		sop["repositoryId"] = repoId

		fmt.Print("Branch Name (required): ")
		branchName, _ := reader.ReadString('\n')
		branchName = strings.TrimSpace(branchName)
		if branchName == "" {
			fmt.Println("Error: Branch Name is required for yunxiao type")
			return sopList
		}
		sop["branchName"] = branchName

		fmt.Print("Base Path (required): ")
		basePath, _ := reader.ReadString('\n')
		basePath = strings.TrimSpace(basePath)
		if basePath == "" {
			fmt.Println("Error: Base Path is required for yunxiao type")
			return sopList
		}
		sop["basePath"] = basePath

		fmt.Print("Token (required): ")
		token, _ := reader.ReadString('\n')
		token = strings.TrimSpace(token)
		if token == "" {
			fmt.Println("Error: Token is required for yunxiao type")
			return sopList
		}
		sop["token"] = token

		fmt.Print("Description (optional): ")
		description, _ := reader.ReadString('\n')
		description = strings.TrimSpace(description)
		if description != "" {
			sop["description"] = description
		}

	case "builtin":
		fmt.Print("ID (required): ")
		id, _ := reader.ReadString('\n')
		id = strings.TrimSpace(id)
		if id == "" {
			fmt.Println("Error: ID is required for builtin type")
			return sopList
		}
		sop["id"] = id

	default:
		fmt.Printf("Unknown SOP type: %s\n", sopType)
		return sopList
	}

	sopList = append(sopList, sop)
	fmt.Println("SOP Knowledge added successfully!")
	return sopList
}

// deleteSopKnowledge 删除一个 SOP Knowledge
func deleteSopKnowledge(sopList []map[string]interface{}, index int) []map[string]interface{} {
	if index < 0 || index >= len(sopList) {
		fmt.Println("Invalid index")
		return sopList
	}

	fmt.Printf("Deleted SOP Knowledge #%d\n", index+1)
	return append(sopList[:index], sopList[index+1:]...)
}

// formatSopKnowledges 格式化显示 SOP Knowledges（用于 employee get）
func formatSopKnowledges(knowledges []map[string]interface{}) string {
	if len(knowledges) == 0 {
		return "  None"
	}

	var result strings.Builder
	for i, sop := range knowledges {
		result.WriteString(fmt.Sprintf("  [%d] ", i+1))

		// 格式化为 JSON 并缩进
		jsonBytes, err := json.MarshalIndent(sop, "      ", "  ")
		if err != nil {
			result.WriteString(fmt.Sprintf("%v\n", sop))
		} else {
			result.WriteString(string(jsonBytes))
			if i < len(knowledges)-1 {
				result.WriteString("\n")
			}
		}
	}

	return result.String()
}
