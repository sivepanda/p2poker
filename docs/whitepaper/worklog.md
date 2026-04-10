# Worklog (for dev use)

Just keeping this here for things to note as y'all work on this.

Once we all are actively working, please make **branches** per **feature**. That'll make it easier to track and implement new things. Scope branches strongly, so don't try to add too many features at once per branch.

Docs are found in the docs folder. Things should be really well documented and fairly easy to understand, but if they are not annoy me about it - siven

You can use [carya](https://github.com/sivepanda/carya) to view my working state & also update go packages on pull lmk if you want me to set it up for u (pls this is good for testing) - Siven

---

`/cmd` should NOT be modified often. It should functionally be ignorable while you work since it's literally just the main method and setup.

`/internal/clientrpc/clientrpcpb` is a *MADE* file made by the MAKEFILE. You do not need to understand the code in here really, and you ABSOLUTELY SHOULD NOT MODIFY it.

## What is [thing]
### Gob???
Gob is a way to do go var -> binary stream -> go var
