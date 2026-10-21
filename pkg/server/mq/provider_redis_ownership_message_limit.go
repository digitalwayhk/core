// 本文件在Broker返回正文前执行字节预算；超限保持PEL，不提前ACK或移动游标回避。
package mq

func redisOwnedSizeBoundScript(script string, pending bool) string {
	selectRows := `for _,stream in ipairs(result) do for _,row in ipairs(stream[2]) do check(row) end end`
	if pending {
		selectRows = `for _,row in ipairs(result[2]) do check(row) end`
	}
	return `local result=(function()
` + script + `
end)()
if not result then return nil end
local limit=tonumber(ARGV[#ARGV])
if not limit or limit<=0 then return redis.error_reply("OWNER_UNKNOWN") end
local function check(row)
 for i=1,#row[2],2 do if row[2][i]=="data" and string.len(row[2][i+1])>limit then error("OWNER_CAPACITY") end end
end
` + selectRows + `
return result`
}
