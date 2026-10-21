"""pathdump.py WORLD FROM TO: the rider's path points from FROM to TO m along the course (x z y, lane, the heading change at each point per step, the step to the next)."""
import json
import math
import sys

d = sys.argv[1]
a, b = float(sys.argv[2]), float(sys.argv[3])
m = json.load(open(d + "/world.json"))
p = m["path"]
step, xyz = p["step_m"], p["xyz"]
lane = p.get("lane_m") or [0] * (len(xyz) // 3)
n = len(xyz) // 3
for i in range(max(int(a / step) - 1, 0), min(int(b / step) + 1, n - 1) + 1):
    x, y, z = xyz[3 * i:3 * i + 3]
    h = ""
    if 0 < i < n - 1:
        ax, az = xyz[3 * (i - 1)], xyz[3 * (i - 1) + 2]
        bx, bz = xyz[3 * (i + 1)], xyz[3 * (i + 1) + 2]
        h1, h2 = math.atan2(x - ax, -(z - az)), math.atan2(bx - x, -(bz - z))
        t = (math.degrees(h2 - h1) + 180) % 360 - 180
        h = f"turn {t:6.1f}°  next {math.hypot(bx - x, bz - z):5.2f} m"
    print(f"{i * step:9.1f}  x {x:9.2f} z {z:9.2f} y {y:7.2f} lane {lane[i]:5.2f}  {h}")
