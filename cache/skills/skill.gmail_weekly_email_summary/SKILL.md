---
name: skill.gmail_weekly_email_summary
description: "Use this skill to find and summarize all emails from the user's Gmail received during the current week when the user requests weekly email summaries. Triggered by phrases like 'tìm tất cả email trong Gmail tuần này và tóm tắt từng cái'. Automates fetching, listing, and summarizing emails by date and sender."
version: 1.0.1
tags: [gmail, email, summary, week, automation]
---

# Gmail Weekly Email Summary

## Workflow
1. Query Gmail for all emails received during the current week.
2. Extract key metadata for each email: sender, date/time, subject.
3. Summarize the content of each email briefly.
4. Present a list with numbered emails and their corresponding summaries.

## Edge cases
- Handle emails with no subject or empty content by providing a placeholder summary.
- Manage large volumes of emails by limiting to the top 20 recent emails.
- Respect user language preference (Vietnamese), replying consistently in Vietnamese.
