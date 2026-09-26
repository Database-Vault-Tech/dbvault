package masking

// FirstNames and LastNames are the pools fake names are drawn from. The SQL
// generated for each engine embeds the same lists in the same order, so do
// not reorder them: that would change every generated name.
var FirstNames = []string{
	"Aarav", "Abena", "Adrian", "Aiko", "Alejandro", "Amara", "Amir", "Ana", "Andrei", "Anika",
	"Arjun", "Astrid", "Ayesha", "Bea", "Bilal", "Bruno", "Camila", "Chen", "Chidi", "Clara",
	"Daniel", "Dara", "Diego", "Elena", "Emeka", "Emma", "Esther", "Farah", "Felix", "Grace",
	"Hana", "Hugo", "Ibrahim", "Ines", "Ivan", "Jamal", "Jin", "Joao", "Julia", "Kai",
	"Kofi", "Lena", "Leo", "Lina", "Luca", "Maria", "Mateo", "Mei", "Nadia", "Nia",
	"Noah", "Omar", "Priya", "Rafael", "Rania", "Rosa", "Sami", "Sara", "Sofia", "Tariq",
	"Thabo", "Tomas", "Wanjiru", "Yara", "Yusuf", "Zara",
}

var LastNames = []string{
	"Abebe", "Adeyemi", "Alvarez", "Andersen", "Bauer", "Brown", "Chowdhury", "Costa", "Dang", "Diallo",
	"Dubois", "Eriksen", "Fernandes", "Fischer", "Garcia", "Gupta", "Haddad", "Hansen", "Ito", "Jensen",
	"Kamau", "Kim", "Kowalski", "Kumar", "Larsen", "Li", "Lopez", "Mbeki", "Meyer", "Moreau",
	"Mwangi", "Nakamura", "Nguyen", "Novak", "Okafor", "Okoye", "Olsen", "Park", "Patel", "Petrov",
	"Popescu", "Rossi", "Santos", "Sato", "Schmidt", "Silva", "Singh", "Suzuki", "Tanaka", "Torres",
	"Van Dijk", "Wang", "Weber", "Wilson", "Yilmaz", "Zhang",
}
