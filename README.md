JKSCTL
-----

# 本工具可以做什么

获取Jenkins信息，为CMDB元信息填充做铺垫

# USAGE

```python
> jksctl -o -l https://demo.com -u username -p password


or 输出到文件


> jksctl -l https://demo.com -u username -p password -f demojenkins.json


or 使用环境变量， 默认是加载当前目录下的.env文件
> jksctl -f demojenkins.json

```
