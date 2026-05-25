// BT Studio — Flutter Dashboard for ThinkTank + Company Workflow
// Connects to Go backend via gRPC/WebSocket

import 'package:flutter/material.dart';
import 'package:grpc/grpc.dart';
import 'dart:convert';

// ─── Models ───

class WorkflowTask {
  final String id, title, description, source, assigneeRole;
  final String priority, status;
  final bool isApproved;
  final int sprintTarget, estimatedEffort;

  WorkflowTask.fromJson(Map<String, dynamic> json)
      : id = json['id'],
        title = json['title'],
        description = json['description'],
        source = json['source'],
        assigneeRole = json['assignee_role'],
        priority = json['priority'],
        status = json['status'],
        isApproved = json['approval']?['is_approved'] ?? false,
        sprintTarget = json['sprint_target'] ?? 1,
        estimatedEffort = json['estimated_effort'] ?? 5;
}

class WorkflowState {
  final String id, name, status;
  final List<WorkflowTask> tasks;
  final int totalTasks, approvedTasks, completedTasks;

  WorkflowState.fromJson(Map<String, dynamic> json)
      : id = json['id'],
        name = json['name'],
        status = json['status'],
        tasks = (json['tasks'] as List).map((t) => WorkflowTask.fromJson(t)).toList(),
        totalTasks = json['tasks'].length,
        approvedTasks = json['tasks'].where((t) => t['approval']?['is_approved'] == true).length,
        completedTasks = json['tasks'].where((t) => t['status'] == 'completed').length;
}

// ─── Main App ───

void main() => runApp(BTStudioApp());

class BTStudioApp extends StatelessWidget {
  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      title: 'BT Studio',
      theme: ThemeData.dark().copyWith(
        colorScheme: ColorScheme.dark(
          primary: Colors.tealAccent,
          secondary: Colors.amber,
          surface: Color(0xFF1E1E2E),
          background: Color(0xFF0D0D1A),
        ),
        cardTheme: CardTheme(color: Color(0xFF1A1A2E), elevation: 2),
      ),
      home: DashboardScreen(),
    );
  }
}

// ─── Dashboard ───

class DashboardScreen extends StatefulWidget {
  @override
  _DashboardScreenState createState() => _DashboardScreenState();
}

class _DashboardScreenState extends State<DashboardScreen> {
  int _selectedIndex = 0;
  WorkflowState? _workflow;

  @override
  void initState() {
    super.initState();
    _loadWorkflow();
  }

  Future<void> _loadWorkflow() async {
    // TODO: connect to Go gRPC backend
    setState(() => _workflow = null); // placeholder
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      body: Row(
        children: [
          NavigationRail(
            selectedIndex: _selectedIndex,
            onDestinationSelected: (i) => setState(() => _selectedIndex = i),
            backgroundColor: Color(0xFF0D0D1A),
            selectedIconTheme: IconThemeData(color: Colors.tealAccent),
            labelType: NavigationRailLabelType.all,
            destinations: [
              NavigationRailDestination(icon: Icon(Icons.dashboard), label: Text('Overview')),
              NavigationRailDestination(icon: Icon(Icons.psychology), label: Text('ThinkTank')),
              NavigationRailDestination(icon: Icon(Icons.business), label: Text('Company')),
              NavigationRailDestination(icon: Icon(Icons.checklist), label: Text('Tasks')),
              NavigationRailDestination(icon: Icon(Icons.account_tree), label: Text('Trees')),
            ],
          ),
          VerticalDivider(width: 1),
          Expanded(child: _buildBody()),
        ],
      ),
    );
  }

  Widget _buildBody() {
    switch (_selectedIndex) {
      case 0: return _buildOverview();
      case 1: return ThinkTankPanel();
      case 2: return CompanyPanel();
      case 3: return TaskPanel(workflow: _workflow);
      case 4: return TreePanel();
      default: return _buildOverview();
    }
  }

  Widget _buildOverview() {
    return SingleChildScrollView(
      padding: EdgeInsets.all(24),
      child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
        Text('BT Studio Dashboard', style: Theme.of(context).textTheme.headlineMedium),
        SizedBox(height: 24),
        Row(children: [
          _MetricCard('Trees', '38', Icons.account_tree, Colors.green),
          SizedBox(width: 16),
          _MetricCard('MCP Tools', '24', Icons.build, Colors.blue),
          SizedBox(width: 16),
          _MetricCard('Tasks', _workflow?.totalTasks.toString() ?? '0', Icons.checklist, Colors.orange),
          SizedBox(width: 16),
          _MetricCard('Sprints', _workflow?.completedTasks.toString() ?? '0', Icons.sprint, Colors.purple),
        ]),
        SizedBox(height: 24),
        Text('Recent Activity', style: Theme.of(context).textTheme.titleLarge),
        SizedBox(height: 12),
        _ActivityCard('ThinkTank Review running', '5 fellows analyzing Hermes Agent', Icons.psychology, Colors.tealAccent),
        _ActivityCard('Sprint 1 completed', 'Visual tree editor MVP shipped', Icons.check_circle, Colors.green),
        _ActivityCard('Genetic evolution cycle', 'Population: 20, Generation: 5', Icons.evolution, Colors.amber),
      ]),
    );
  }

  Widget _MetricCard(String label, String value, IconData icon, Color color) {
    return Expanded(
      child: Card(
        child: Padding(
          padding: EdgeInsets.all(16),
          child: Column(children: [
            Icon(icon, color: color, size: 32),
            SizedBox(height: 8),
            Text(value, style: TextStyle(fontSize: 28, fontWeight: FontWeight.bold)),
            Text(label, style: TextStyle(color: Colors.grey)),
          ]),
        ),
      ),
    );
  }

  Widget _ActivityCard(String title, String subtitle, IconData icon, Color color) {
    return Card(
      child: ListTile(
        leading: Icon(icon, color: color),
        title: Text(title),
        subtitle: Text(subtitle),
        trailing: Icon(Icons.chevron_right),
      ),
    );
  }
}

// ─── ThinkTank Panel ───

class ThinkTankPanel extends StatelessWidget {
  @override
  Widget build(BuildContext context) {
    return SingleChildScrollView(
      padding: EdgeInsets.all(24),
      child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
        Row(children: [
          Icon(Icons.psychology, color: Colors.tealAccent, size: 32),
          SizedBox(width: 12),
          Text('ThinkTank', style: Theme.of(context).textTheme.headlineMedium),
          Spacer(),
          ElevatedButton.icon(
            onPressed: () {},
            icon: Icon(Icons.add),
            label: Text('New Analysis'),
            style: ElevatedButton.styleFrom(backgroundColor: Colors.tealAccent),
          ),
        ]),
        SizedBox(height: 24),
        Text('Active Fellows', style: Theme.of(context).textTheme.titleLarge),
        SizedBox(height: 12),
        _FellowCard('Victoria Bull', 'Optimistic growth thesis', 0.8, Colors.green),
        _FellowCard('Marcus Bear', 'Skeptical risk assessment', 0.85, Colors.red),
        _FellowCard('Dr. Elena Tech', 'Deep technical evaluation', 0.75, Colors.blue),
        _FellowCard('Prof. James Macro', 'Systems-level macro analysis', 0.7, Colors.purple),
        _FellowCard('Sophia Contrarian', 'Challenges consensus', 0.9, Colors.orange),
        SizedBox(height: 24),
        Text('Synthesis', style: Theme.of(context).textTheme.titleLarge),
        SizedBox(height: 12),
        Card(
          child: Padding(
            padding: EdgeInsets.all(16),
            child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
              Text('Thesis', style: TextStyle(color: Colors.green, fontWeight: FontWeight.bold)),
              Text('Hermes Agent framework is uniquely positioned with BT integration'),
              SizedBox(height: 8),
              Text('Antithesis', style: TextStyle(color: Colors.red, fontWeight: FontWeight.bold)),
              Text('Jetson CPU bottleneck limits practical utility'),
              SizedBox(height: 8),
              Text('Synthesis', style: TextStyle(color: Colors.tealAccent, fontWeight: FontWeight.bold)),
              Text('Wire real tools, add GPU inference, prune unused trees'),
            ]),
          ),
        ),
      ]),
    );
  }

  Widget _FellowCard(String name, String perspective, double confidence, Color color) {
    return Card(
      child: ListTile(
        leading: CircleAvatar(backgroundColor: color, child: Text(name[0], style: TextStyle(color: Colors.white))),
        title: Text(name),
        subtitle: Text(perspective),
        trailing: Column(mainAxisAlignment: MainAxisAlignment.center, children: [
          Text('${(confidence * 100).toInt()}%', style: TextStyle(fontWeight: FontWeight.bold)),
          Text('confidence', style: TextStyle(fontSize: 10, color: Colors.grey)),
        ]),
      ),
    );
  }
}

// ─── Company Panel ───

class CompanyPanel extends StatelessWidget {
  @override
  Widget build(BuildContext context) {
    return SingleChildScrollView(
      padding: EdgeInsets.all(24),
      child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
        Text('BT Studio Inc.', style: Theme.of(context).textTheme.headlineMedium),
        SizedBox(height: 8),
        Text('Pre-seed · 4 team · 8mo runway', style: TextStyle(color: Colors.grey)),
        SizedBox(height: 24),
        Row(children: [
          _MetricCard('MRR', '\$18k', Icons.trending_up, Colors.green),
          SizedBox(width: 16),
          _MetricCard('Users', '1.2k', Icons.people, Colors.blue),
          SizedBox(width: 16),
          _MetricCard('Runway', '14mo', Icons.timer, Colors.orange),
          SizedBox(width: 16),
          _MetricCard('Burn', '\$45k', Icons.money_off, Colors.red),
        ]),
        SizedBox(height: 24),
        Text('Current Sprint', style: Theme.of(context).textTheme.titleLarge),
        SizedBox(height: 12),
        Card(
          child: ListTile(
            leading: Icon(Icons.sprint, color: Colors.tealAccent),
            title: Text('Sprint 12: Launch enterprise SSO'),
            subtitle: Text('4 engineers · 2-week sprint'),
            trailing: Chip(label: Text('In Progress')),
          ),
        ),
      ]),
    );
  }
}

// ─── Task Panel with Approval ───

class TaskPanel extends StatelessWidget {
  final WorkflowState? workflow;

  const TaskPanel({this.workflow});

  @override
  Widget build(BuildContext context) {
    return SingleChildScrollView(
      padding: EdgeInsets.all(24),
      child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
        Row(children: [
          Text('Tasks', style: Theme.of(context).textTheme.headlineMedium),
          Spacer(),
          PopupMenuButton<String>(
            onSelected: (v) {},
            itemBuilder: (ctx) => ['All', 'Pending Approval', 'Approved', 'In Progress', 'Completed']
                .map((s) => PopupMenuItem(value: s, child: Text(s))).toList(),
          ),
        ]),
        SizedBox(height: 16),
        if (workflow != null)
          ...workflow!.tasks.map((task) => _TaskCard(task: task)),
        if (workflow == null)
          Text('No workflow loaded', style: TextStyle(color: Colors.grey)),
      ]),
    );
  }
}

class _TaskCard extends StatelessWidget {
  final WorkflowTask task;

  const _TaskCard({required this.task});

  @override
  Widget build(BuildContext context) {
    final isPending = task.status == 'pending';
    final isApproved = task.isApproved;

    return Card(
      child: Padding(
        padding: EdgeInsets.all(12),
        child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
          Row(children: [
            _PriorityBadge(task.priority),
            SizedBox(width: 8),
            Expanded(child: Text(task.title, style: TextStyle(fontWeight: FontWeight.w600))),
          ]),
          SizedBox(height: 8),
          Row(children: [
            Chip(label: Text(task.assigneeRole, style: TextStyle(fontSize: 11)), materialTapTargetSize: MaterialTapTargetSize.shrinkWrap),
            SizedBox(width: 8),
            Chip(label: Text('Sprint ${task.sprintTarget}', style: TextStyle(fontSize: 11)), materialTapTargetSize: MaterialTapTargetSize.shrinkWrap),
            SizedBox(width: 8),
            Chip(label: Text('${task.estimatedEffort} SP', style: TextStyle(fontSize: 11)), materialTapTargetSize: MaterialTapTargetSize.shrinkWrap),
            Spacer(),
            if (isPending)
              Row(mainAxisSize: MainAxisSize.min, children: [
                _ApproveButton(task: task),
                SizedBox(width: 4),
                _RejectButton(task: task),
              ]),
            if (isApproved) Icon(Icons.check_circle, color: Colors.green, size: 20),
          ]),
        ]),
      ),
    );
  }
}

class _PriorityBadge extends StatelessWidget {
  final String priority;
  const _PriorityBadge(this.priority);

  @override
  Widget build(BuildContext context) {
    final colors = {
      'critical': Colors.red,
      'high': Colors.orange,
      'medium': Colors.amber,
      'low': Colors.grey,
    };
    return Container(
      padding: EdgeInsets.symmetric(horizontal: 8, vertical: 2),
      decoration: BoxDecoration(
        color: colors[priority] ?? Colors.grey,
        borderRadius: BorderRadius.circular(4),
      ),
      child: Text(priority.toUpperCase(), style: TextStyle(fontSize: 10, fontWeight: FontWeight.bold, color: Colors.white)),
    );
  }
}

class _ApproveButton extends StatelessWidget {
  final WorkflowTask task;
  const _ApproveButton({required this.task});

  @override
  Widget build(BuildContext context) {
    return SizedBox(
      width: 28, height: 28,
      child: IconButton(
        onPressed: () {},
        icon: Icon(Icons.check, size: 16, color: Colors.green),
        padding: EdgeInsets.zero,
        tooltip: 'Approve',
      ),
    );
  }
}

class _RejectButton extends StatelessWidget {
  final WorkflowTask task;
  const _RejectButton({required this.task});

  @override
  Widget build(BuildContext context) {
    return SizedBox(
      width: 28, height: 28,
      child: IconButton(
        onPressed: () {},
        icon: Icon(Icons.close, size: 16, color: Colors.red),
        padding: EdgeInsets.zero,
        tooltip: 'Reject',
      ),
    );
  }
}

// ─── Tree Panel ───

class TreePanel extends StatelessWidget {
  @override
  Widget build(BuildContext context) {
    return SingleChildScrollView(
      padding: EdgeInsets.all(24),
      child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
        Text('Behavior Trees', style: Theme.of(context).textTheme.headlineMedium),
        SizedBox(height: 16),
        Text('38 trees · 7 categories', style: TextStyle(color: Colors.grey)),
        SizedBox(height: 16),
        _TreeCategoryCard('Finance', '10 trees', Icons.attach_money, Colors.green),
        _TreeCategoryCard('Domain', '10 trees', Icons.code, Colors.blue),
        _TreeCategoryCard('Startup', '6 trees', Icons.business, Colors.purple),
        _TreeCategoryCard('ThinkTank', '5 trees', Icons.psychology, Colors.tealAccent),
        _TreeCategoryCard('Evolution', '3 trees', Icons.evolution, Colors.amber),
        _TreeCategoryCard('Research', '2 trees', Icons.science, Colors.orange),
        _TreeCategoryCard('Core', '2 trees', Icons.home, Colors.grey),
      ]),
    );
  }

  Widget _TreeCategoryCard(String name, String count, IconData icon, Color color) {
    return Card(
      child: ListTile(
        leading: Icon(icon, color: color),
        title: Text(name),
        subtitle: Text(count),
        trailing: Icon(Icons.chevron_right),
      ),
    );
  }
}
