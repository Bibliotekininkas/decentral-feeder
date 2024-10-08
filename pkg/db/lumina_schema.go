package db

var LuminaDBSchema = `database lumina;

table exchange {
	exchange_id uuid primary notnull,
	name text unique notnull,
	centralized bool default(false),
	blockchain text
}
  
procedure set_exchange($name text,$centralized bool,$blockchain text) public {
    $id uuid := uuid_generate_v5('455f60aa-0569-4aaa-8469-63be2ec4dd96'::uuid, @txid);
	INSERT INTO "exchange" (exchange_id,name,centralized,blockchain) VALUES ($id,$name,$centralized,$blockchain);
}
  
action get_all_exchanges() public view {
	SELECT name,centralized,blockchain FROM exchange;
}

action get_exchange($name) public view {
	SELECT centralized,blockchain FROM exchange WHERE name=$name;
}

`
