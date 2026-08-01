import React, { useState } from 'react';
import { View, Text, StyleSheet, Pressable, ActivityIndicator } from 'react-native';
import { useRouter } from 'expo-router';
import { inr } from '../../src/data/mock';
import { useOwnerData, type OwnerData } from '../../src/data/source';
import {
  ActivityRow,
  AppHeader,
  AvatarButton,
  BarChart,
  Card,
  ChevronDown,
  DocIcon,
  DottedRule,
  MoneyRow,
  ProgressBar,
  Screen,
  SectionTitle,
  ListRow,
  Segmented,
  color,
  font,
  radius,
  space,
  ui,
} from '@dwellm8/mobile-shared';

export default function Financials() {
  const router = useRouter();
  const [tab, setTab] = useState('Dashboard');
  const data = useOwnerData();
  const scope = data.properties[0];

  return (
    <>
      <AppHeader
        title={scope?.address ?? 'Financials'}
        onTitlePress={() => router.push('/switcher')}
        left={<AvatarButton onPress={() => router.push('/profile')} />}
      />
      <View style={{ backgroundColor: '#FFF', paddingBottom: space(3) }}>
        <Segmented items={['Dashboard', 'Statements', 'Transactions']} value={tab} onChange={setTab} />
      </View>

      <Screen>
        {data.loading ? (
          <View style={{ paddingTop: 120, alignItems: 'center' }}>
            <ActivityIndicator />
          </View>
        ) : (
          <>
            {tab === 'Dashboard' ? <Dashboard data={data} /> : null}
            {tab === 'Statements' ? <Statements data={data} /> : null}
            {tab === 'Transactions' ? <Transactions data={data} /> : null}
          </>
        )}
      </Screen>
    </>
  );
}

function Dashboard({ data }: { data: OwnerData }) {
  const router = useRouter();
  const { balance, chart, chartMax, expenseBreakdown } = data;
  const current = chart[chart.length - 1] ?? { label: '', income: 0, expense: 0 };
  const totalExpense = Math.max(1, expenseBreakdown.reduce((a, b) => a + b.paise, 0));

  return (
    <>
      <View style={s.band}>
        <SectionTitle style={{ marginTop: space(4) }}>Current Balance</SectionTitle>
        <Card>
          <MoneyRow label="Net balance" value={inr(balance.net)} strong />
          <MoneyRow label="Opening balance" value={inr(balance.opening)} />
          <MoneyRow label="Money in" value={inr(balance.moneyIn)} tone="positive" />
          <MoneyRow label="Money out" value={inr(balance.moneyOut)} tone="negative" />
          <MoneyRow label="Uncleared funds" value={inr(balance.uncleared)} />
          <MoneyRow label="Unpaid bills" value={inr(balance.unpaidBills)} tone="negative" last />
        </Card>
      </View>

      <SectionTitle>Insights</SectionTitle>
      <Text style={s.caption}>Current &amp; archived properties</Text>
      <Pressable style={s.select}>
        <Text style={s.selectText}>Last 6 months</Text>
        <ChevronDown size={20} />
      </Pressable>

      <SectionTitle>Income vs Expenses</SectionTitle>
      <Card>
        <Text style={s.monthLabel}>{current.label ? `${current.label} ${new Date().getFullYear()}` : 'This month'}</Text>
        <View style={{ flexDirection: 'row', gap: 14, marginTop: 3, marginBottom: space(4) }}>
          <Text style={[s.legend, { color: color.positive }]}>Income: {inr(current.income)}</Text>
          <Text style={[s.legend, { color: color.negative }]}>Expenses: {inr(current.expense)}</Text>
        </View>
        <BarChart data={chart} max={chartMax} />
        <Text style={s.year}>{String(new Date().getFullYear())}</Text>
      </Card>

      <SectionTitle>Expenses Breakdown</SectionTitle>
      <Card>
        {expenseBreakdown.map((e, i) => (
          <View key={e.label} style={{ paddingVertical: space(2) }}>
            <View style={s.expenseRow}>
              <Text style={s.expenseLabel} numberOfLines={1}>{e.label}</Text>
              <Text style={s.expenseValue}>{inr(e.paise)}</Text>
            </View>
            <ProgressBar pct={(e.paise / totalExpense) * 100} tint={e.tint} />
            {i < expenseBreakdown.length - 1 ? <View style={{ height: space(2) }} /> : null}
          </View>
        ))}
      </Card>

      <SectionTitle>Also here</SectionTitle>
      <Card padded={false} style={{ paddingHorizontal: space(4) }}>
        <ListRow
          title="Payouts"
          subtitle="What reached your bank, and the deductions behind each one"
          onPress={() => router.push('/payouts')}
        />
        <ListRow
          title="Tax pack 2025–26"
          subtitle="Income from house property, TDS credit and the certificates"
          onPress={() => router.push('/tax')}
        />
        <ListRow
          title="Documents"
          subtitle="Statements, agreements, compliance and tax"
          onPress={() => router.push('/documents')}
          last
        />
      </Card>

    </>
  );
}

function Statements({ data }: { data: OwnerData }) {
  const { statements, properties } = data;
  const byMonth = statements.reduce<Record<string, typeof statements>>((acc, st) => {
    (acc[st.month] ||= []).push(st);
    return acc;
  }, {});

  if (!statements.length) {
    return <SectionTitle>No statements yet</SectionTitle>;
  }
  return (
    <>
      <SectionTitle>{String(new Date().getFullYear())}</SectionTitle>
      {Object.entries(byMonth).map(([month, list]) => (
        <View key={month}>
          <Text style={s.monthHeading}>{month}</Text>
          {list.map((st) => (
            <Card key={st.id} padded={false}>
              <View style={s.stHead}>
                <Text style={s.stHeadText}>{properties.find((p) => p.id === st.propertyId)?.address}</Text>
              </View>
              <View style={{ padding: space(4), paddingTop: space(1) }}>
                {st.lines.map((l, i) => (
                  <MoneyRow
                    key={l.label}
                    label={l.label}
                    value={inr(l.paise)}
                    tone={l.paise >= 0 ? 'positive' : 'negative'}
                    last={i === st.lines.length - 1}
                  />
                ))}
                <View style={{ height: space(2) }} />
                <DottedRule />
                <MoneyRow label={`Statement ${st.n} · ${st.date}`} value={inr(st.netPaise)} strong last />
              </View>
            </Card>
          ))}
        </View>
      ))}
    </>
  );
}

function Transactions({ data }: { data: OwnerData }) {
  const { statements } = data;
  return (
    <>
      <SectionTitle>Recent transactions</SectionTitle>
      {statements.map((st) => (
        <ActivityRow
          key={st.id}
          icon={<DocIcon size={22} c={color.inkFaint} />}
          title={`Statement ${st.n} — ${inr(st.netPaise)}`}
          subtitle={
            <Text style={{ ...font.small, marginTop: 3 }}>
              <Text style={{ color: color.positive }}>Income {inr(st.incomePaise)}</Text>
              <Text style={{ color: color.inkFaint }}>{'  •  '}</Text>
              <Text style={{ color: color.negative }}>Expense {inr(st.expensePaise)}</Text>
            </Text>
          }
          meta={st.date}
        />
      ))}
    </>
  );
}

const s = StyleSheet.create({
  band: { backgroundColor: color.bgBand, paddingBottom: space(4) },
  caption: { ...font.body, color: color.ink, marginHorizontal: space(4), marginBottom: space(3) },
  select: {
    flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between',
    backgroundColor: '#FFF', borderRadius: radius.pill, borderWidth: 1, borderColor: color.line,
    paddingHorizontal: space(5), paddingVertical: space(4), marginHorizontal: space(4),
  },
  selectText: { ...font.h3, color: color.inkStrong, fontWeight: '600' },
  monthLabel: { ...font.h3, color: color.inkStrong },
  legend: { ...font.small, fontWeight: '700' },
  year: { ...font.label, color: color.inkStrong, textAlign: 'center', marginTop: space(2) },
  expenseRow: { flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center', gap: 10 },
  expenseLabel: { ...font.body, color: color.inkStrong, flexShrink: 1 },
  expenseValue: { ...font.body, color: color.inkStrong, fontWeight: '700' },
  monthHeading: { ...font.h3, color: color.inkStrong, marginHorizontal: space(4), marginBottom: space(2), marginTop: space(2) },
  stHead: {
    backgroundColor: '#E7ECF2', paddingVertical: space(3), alignItems: 'center',
    borderTopLeftRadius: radius.lg, borderTopRightRadius: radius.lg,
  },
  stHeadText: { ...font.label, color: color.inkStrong },
});
