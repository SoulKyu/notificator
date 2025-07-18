# Notificator Collaboration Features

So you want to use Notificator with your team? Great! Here's everything you need to know about the collaboration features.

## Why use collaboration mode?

Picture this: It's 3 AM, alerts are firing, and you're not sure if your colleague is already handling it or if they're still asleep. With collaboration mode, everyone on your team sees the same alerts in real-time, can acknowledge them, and leave notes for each other.

No more "are you looking at this?" messages in Slack!

## What you get

### User Authentication
Everyone gets their own login. No more anonymous alert handling!

![User Registration](../screenshots/user-registration.png)
*Screenshot: Registration dialog with username/password fields*

![User Login](../screenshots/user-login.png)
*Screenshot: Login dialog with username/password fields*

### Alert Acknowledgments
Click "Ack" on any alert to let your team know you're handling it. The alert gets marked with your name and timestamp.

![Alert Acknowledgment](../screenshots/alert-ack.png)
*Screenshot: Alert with acknowledge button and "Acked by Bastien at 14:32" status*

### Comments on Alerts
Add context to alerts! Leave notes about what you're investigating, what you found, or what actions you took.

![Alert Comments](../screenshots/alert-comments.png)
*Screenshot: Alert with comment section showing conversation between team members*

### Team Filters
Filter alerts by acknowledgment status to quickly see what needs attention:
- Show only unacknowledged alerts
- Show only my acknowledged alerts
- Show alerts with comments

### Alert History
See the full timeline of what happened with each alert - when it fired, who acknowledged it, what comments were added.

## Troubleshooting

### "Can't connect to backend"
- Check if the backend is running: `telnet backend-server 50051`
- Verify the grpc_client address in your config
- Make sure port 50051 is open in firewall

### "Login failed"
- Check username/password
- Make sure the backend database is accessible
- Look at backend logs for error messages

### "Not seeing real-time updates"
- Restart your desktop client
- Check if backend is responding: `curl http://backend-server:8080/health`
- Network issues? Check if gRPC traffic is blocked

## Security notes

### Database
- Use PostgreSQL in production (not SQLite)
- Secure your database connection (SSL, firewall)
- Regular backups (includes user data and alert history)

### Network
- Backend should be on internal network
- Consider VPN for remote team members
- gRPC uses port 50051 (secure with TLS in production)

### User management
- Currently no admin UI (manage users directly in database)
- Users can only see alerts, not change Alertmanager config
- No role-based permissions yet (all users are equal)

## Deployment options

### Simple VM setup
```bash
# On your server
./notificator --backend --db postgres
```

### Kubernetes deployment
Use the Helm chart in `helm/notificator-backend/`:
```bash
helm install notificator-backend ./helm/notificator-backend
```
