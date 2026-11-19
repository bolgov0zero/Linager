package agent

// processDescendants returns the processes below root in the process tree, the deepest first, so that ending them in that order
// never leaves a child to be adopted. parent maps a PID to the PID of its parent and created to its start time (unix ms).
// A process whose parent started later than it did is not a child: its parent has ended and the PID was given to another process.
func processDescendants(parent map[int32]int32, created map[int32]int64, root int32) []int32 {
	children := make(map[int32][]int32)
	for pid, ppid := range parent {
		if pid == ppid || pid == root {
			continue
		}
		if cp, ok := created[pid]; ok {
			if pp, ok := created[ppid]; ok && cp < pp {
				continue
			}
		}
		children[ppid] = append(children[ppid], pid)
	}
	var order []int32
	seen := map[int32]bool{root: true}
	queue := []int32{root}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, c := range children[cur] {
			if seen[c] {
				continue
			}
			seen[c] = true
			order = append(order, c)
			queue = append(queue, c)
		}
	}
	for i, j := 0, len(order)-1; i < j; i, j = i+1, j-1 {
		order[i], order[j] = order[j], order[i]
	}
	return order
}
